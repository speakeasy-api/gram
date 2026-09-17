package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// errWorkloadSessionCredentialLoad marks a failure to read the stored session
// row, so a database outage is reported as operational rather than as a caller
// presenting a bad credential.
var errWorkloadSessionCredentialLoad = errors.New("load workload session credential")

// errWorkloadSessionAdmissionLoad marks an admission that could not reach a
// decision, such as a failed policy read, so it is not reported as the
// workload's token having been withdrawn.
var errWorkloadSessionAdmissionLoad = errors.New("load workload session admission")

// workloadSessionCredential is the immutable ceiling a workload session was
// minted with. It carries no authorizer: a workload records no approving human,
// which is where it differs from an agent credential.
type workloadSessionCredential struct {
	DelegatedGrants        []byte
	DelegatedGrantsVersion int32
}

// loadWorkloadSessionCredential validates the stored ceiling against the
// endpoint the session is being presented to.
//
// The stored policy is re-encoded and compared byte for byte with the policy
// this endpoint would mint, so a row edited to name another resource cannot
// authorize anything here. Same treatment as an agent session, minus the
// authorizer.
func loadWorkloadSessionCredential(
	endpoint *ResolvedMcpEndpoint,
	subject urn.SessionSubject,
	storedSubject urn.SessionSubject,
	organizationID pgtype.Text,
	delegatedGrants []byte,
	delegatedGrantsVersion pgtype.Int4,
) (workloadSessionCredential, error) {
	if subject.Kind != urn.SessionSubjectKindWorkload || subject.String() != storedSubject.String() ||
		!organizationID.Valid || organizationID.String != endpoint.OrganizationID ||
		delegatedGrants == nil || !delegatedGrantsVersion.Valid {
		return workloadSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}
	if _, _, err := subject.Workload(); err != nil {
		return workloadSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}

	version := runtimepolicy.DelegatedPolicyVersion(delegatedGrantsVersion.Int32)
	decoded, err := runtimepolicy.DecodeDelegatedPolicy(version, delegatedGrants)
	if err != nil {
		return workloadSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}
	normalized, err := runtimepolicy.EncodeDelegatedPolicy(version, decoded)
	if err != nil {
		return workloadSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}
	target, ok := agentAuthorizationTarget(endpoint)
	if !ok {
		return workloadSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}
	expected, err := encodeAgentSessionPolicy(*target, version)
	if err != nil || !bytes.Equal(normalized, expected) {
		return workloadSessionCredential{}, oops.C(oops.CodeUnauthorized)
	}

	return workloadSessionCredential{
		DelegatedGrants:        append([]byte(nil), delegatedGrants...),
		DelegatedGrantsVersion: delegatedGrantsVersion.Int32,
	}, nil
}

// isCredentialDenial reports whether an authorization error is the request
// being refused rather than the decision failing. Only the two durable denial
// codes qualify: an engine that answers CodeUnexpected has judged nothing, and
// treating that as a refusal would tell a workload to discard a live token
// every time the policy store wobbles.
func isCredentialDenial(err error) bool {
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	if !ok {
		return false
	}
	return shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized
}

func (s *Service) prepareWorkloadSessionContext(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, credential workloadSessionCredential) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || subject.Kind != urn.SessionSubjectKindWorkload {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	// A workload acts through an agent's policy, so it rides the agent
	// authorization rollout and is hidden the same way while that is off. The
	// token is untouched by either outcome, so both stay unmarked: a fresh one
	// would meet the same gate.
	enabled, _, rolloutErr := s.agentAuthorizationRollout(ctx, s.logger, endpoint)
	switch {
	case rolloutErr != nil:
		// The rollout state could not be read, so nothing here says the
		// feature is off for this organization.
		return ctx, fmt.Errorf("%w: %w", errWorkloadRolloutUnavailable, oops.C(oops.CodeNotFound))
	case !enabled:
		return ctx, fmt.Errorf("%w: %w", errWorkloadRolloutDisabled, oops.C(oops.CodeNotFound))
	}
	workloadIssuerID, externalSubject, err := subject.Workload()
	if err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	actor := urn.NewWorkloadPrincipal(workloadIssuerID, externalSubject)
	ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, authCtx, actor, contextvalues.PrincipalCredential{
		// No authorizer, so admission must not require one.
		AuthorizerUserID:       "",
		DelegatedGrants:        credential.DelegatedGrants,
		DelegatedGrantsVersion: credential.DelegatedGrantsVersion,
	})
	return ctx, nil
}

func (s *Service) admitWorkloadSession(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, credential workloadSessionCredential) (context.Context, error) {
	ctx, err := s.prepareWorkloadSessionContext(ctx, endpoint, subject, credential)
	if err != nil {
		return ctx, err
	}
	ctx, err = s.authz.PrepareContext(ctx)
	if err != nil {
		// A denial means the workload is no longer admitted, which its token
		// cannot fix. Anything else — including a shareable error carrying an
		// internal code — reached no decision, so the credential keeps the
		// benefit of the doubt.
		if isCredentialDenial(err) {
			err = fmt.Errorf("%w: %w", errCredentialRejected, err)
		} else {
			err = fmt.Errorf("%w: %w", errWorkloadSessionAdmissionLoad, err)
		}
		return ctx, fmt.Errorf("prepare workload session authorization: %w", err)
	}
	return s.requireWorkloadSessionAuthorization(ctx, endpoint)
}

func (s *Service) requireWorkloadSessionAuthorization(ctx context.Context, endpoint *ResolvedMcpEndpoint) (context.Context, error) {
	target, ok := agentAuthorizationTarget(endpoint)
	if !ok {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, target.connectCheck()); err != nil {
		// A denial is the assigned agent's authority being gone or withdrawn;
		// an engine failure reached no decision and stays unmarked.
		if isCredentialDenial(err) {
			return ctx, fmt.Errorf("%w: %w", errCredentialRejected, err)
		}
		return ctx, err
	}
	return ctx, nil
}
