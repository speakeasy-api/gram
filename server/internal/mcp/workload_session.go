package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
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

// errWorkloadSessionOutOfReach marks a live workload session presented to an
// MCP server it does not reach, when it was minted for every MCP server of its
// issuer: the assigned agent may not connect to this one, or the endpoint
// cannot carry a workload session policy at all. The token still works at the
// issuer's other servers, and a fresh one would be refused here too, so it is
// answered with 403 rather than invalid_token.
var errWorkloadSessionOutOfReach = errors.New("workload session does not reach this MCP server")

// workloadSessionReach is which MCP servers a workload session was minted for.
type workloadSessionReach string

const (
	// workloadSessionReachResource is the one MCP server the grant named as
	// its RFC 8707 resource.
	workloadSessionReachResource workloadSessionReach = "resource"

	// workloadSessionReachIssuer is every MCP server of the user session
	// issuer whose shared authorization server minted the session, for a
	// grant that named no resource.
	workloadSessionReachIssuer workloadSessionReach = "issuer"
)

// workloadSessionCredential is the immutable ceiling a workload session was
// minted with. It carries no authorizer: a workload records no approving human,
// which is where it differs from an agent credential.
type workloadSessionCredential struct {
	DelegatedGrants        []byte
	DelegatedGrantsVersion int32
}

// encodeIssuerWorkloadSessionPolicy is the ceiling of a workload session
// minted for every MCP server of a user session issuer: mcp:connect on every
// MCP server in the issuer's project, or in its organization when
// issuerProjectID is uuid.Nil.
//
// It deliberately bounds nothing finer. The session's audience already
// confines it to the issuer's MCP servers, and which of those it reaches is
// the assigned agent's live policy, which authorization intersects with this
// ceiling on every request.
func encodeIssuerWorkloadSessionPolicy(issuerProjectID uuid.UUID, version runtimepolicy.DelegatedPolicyVersion) ([]byte, error) {
	selector := authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
		authz.SelectorKeyResourceID:   authz.WildcardResource,
	}
	if issuerProjectID != uuid.Nil {
		selector[authz.SelectorKeyProjectID] = issuerProjectID.String()
	}
	policy, err := runtimepolicy.NewDelegatedPolicy(version, []authz.Grant{{
		PrincipalUrn: "",
		Scope:        authz.ScopeMCPConnect,
		Selector:     selector,
		Inferred:     false,
	}})
	if err != nil {
		return nil, fmt.Errorf("construct issuer workload session policy: %w", err)
	}
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(version, policy)
	if err != nil {
		return nil, fmt.Errorf("encode issuer workload session policy: %w", err)
	}
	return encoded, nil
}

// loadWorkloadSessionCredential validates the stored ceiling of a session
// minted for the endpoint's own resource, one MCP server or a stored gateway,
// against the endpoint it is being presented to.
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
	return checkWorkloadSessionCredential(endpoint.OrganizationID, subject, storedSubject, organizationID, delegatedGrants, delegatedGrantsVersion, func(version runtimepolicy.DelegatedPolicyVersion) ([]byte, error) {
		policy, ok, err := endpointWorkloadSessionPolicy(endpoint, version)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("endpoint cannot carry a workload session policy")
		}
		return policy, nil
	})
}

// loadIssuerWorkloadSessionCredential validates the stored ceiling of a
// session minted for every MCP server of a user session issuer, in the
// issuer's organization and, for a project issuer, its project.
//
// The comparison is byte for byte, as for a session minted for one server, so
// only the ceiling encodeIssuerWorkloadSessionPolicy mints for this issuer is
// accepted. In particular a single-server ceiling never rides an issuer-wide
// audience.
func loadIssuerWorkloadSessionCredential(
	issuerOrganizationID string,
	issuerProjectID uuid.UUID,
	subject urn.SessionSubject,
	storedSubject urn.SessionSubject,
	organizationID pgtype.Text,
	delegatedGrants []byte,
	delegatedGrantsVersion pgtype.Int4,
) (workloadSessionCredential, error) {
	return checkWorkloadSessionCredential(issuerOrganizationID, subject, storedSubject, organizationID, delegatedGrants, delegatedGrantsVersion, func(version runtimepolicy.DelegatedPolicyVersion) ([]byte, error) {
		return encodeIssuerWorkloadSessionPolicy(issuerProjectID, version)
	})
}

// checkWorkloadSessionCredential validates a stored workload session row:
// the subject it was minted for, its organization, and a ceiling equal to the
// one expected encodes for its version.
func checkWorkloadSessionCredential(
	expectedOrganizationID string,
	subject urn.SessionSubject,
	storedSubject urn.SessionSubject,
	organizationID pgtype.Text,
	delegatedGrants []byte,
	delegatedGrantsVersion pgtype.Int4,
	expected func(runtimepolicy.DelegatedPolicyVersion) ([]byte, error),
) (workloadSessionCredential, error) {
	if subject.Kind != urn.SessionSubjectKindWorkload || subject.String() != storedSubject.String() ||
		!organizationID.Valid || expectedOrganizationID == "" || organizationID.String != expectedOrganizationID ||
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
	want, err := expected(version)
	if err != nil || !bytes.Equal(normalized, want) {
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

func (s *Service) prepareWorkloadSessionContext(ctx context.Context, organizationID string, subject urn.SessionSubject, credential workloadSessionCredential) (context.Context, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || subject.Kind != urn.SessionSubjectKindWorkload {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	workloadIssuerID, externalSubject, err := subject.Workload()
	if err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	actor := urn.NewWorkloadPrincipal(workloadIssuerID, externalSubject)
	ctx = contextvalues.WithPrincipalCredentialAuthorization(ctx, authCtx, actor, contextvalues.PrincipalCredential{
		// No authorizer, so admission must not require one.
		AuthorizerUserID:         "",
		DelegatedGrants:          credential.DelegatedGrants,
		DelegatedGrantsVersion:   credential.DelegatedGrantsVersion,
		DerivesPolicyAtAdmission: false,
	})
	return ctx, nil
}

// admitWorkloadSession admits a workload session in the organization it acts
// in: the workload still has a live assigned agent with an eligible owner. It does not decide whether the session reaches a
// particular MCP server; requireWorkloadSessionAuthorization does.
func (s *Service) admitWorkloadSession(ctx context.Context, organizationID string, subject urn.SessionSubject, credential workloadSessionCredential) (context.Context, error) {
	ctx, err := s.prepareWorkloadSessionContext(ctx, organizationID, subject, credential)
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
	return ctx, nil
}

// requireWorkloadSessionAuthorization checks an admitted workload session may
// connect to the endpoint's MCP server.
//
// A session minted for this one server is withdrawn when it may not: nothing
// else could accept it. A session minted for all of the issuer's servers is
// only out of reach here, and stays valid at the others.
//
// A stored gateway has no single server to check. Either session enters it,
// and each request admits every live member against the same three policies.
// A gateway session whose agent has lost every member is therefore served an
// empty member list rather than withdrawn with invalid_token, as a
// single-server session is when its one server refuses it. A gateway's
// members and grants change independently of the session, so a member granted
// or added later serves the same session until it expires; an empty gateway
// says nothing final about the token.
func (s *Service) requireWorkloadSessionAuthorization(ctx context.Context, endpoint *ResolvedMcpEndpoint, reach workloadSessionReach) (context.Context, error) {
	if agentGatewayAddressable(endpoint) {
		return ctx, nil
	}
	target, ok := agentAuthorizationTarget(endpoint)
	if !ok {
		if reach == workloadSessionReachIssuer {
			return ctx, fmt.Errorf("%w: endpoint cannot carry an agent session policy", errWorkloadSessionOutOfReach)
		}
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, target.connectCheck()); err != nil {
		// A denial is the assigned agent's authority being gone or withdrawn;
		// an engine failure reached no decision and stays unmarked.
		if !isCredentialDenial(err) {
			return ctx, err
		}
		if reach == workloadSessionReachIssuer {
			return ctx, fmt.Errorf("%w: %w", errWorkloadSessionOutOfReach, err)
		}
		return ctx, fmt.Errorf("%w: %w", errCredentialRejected, err)
	}
	return ctx, nil
}
