// The workload assertion grant at the device-agent token endpoint: a headless
// device exchanges its identity assertion for a bearer token it presents to
// the device-agent API (plugin policy and hooks) instead of an agent API key.

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// deviceAgentAuthorizationServerNamespace keys lookup, key-fetch and replay
// budgets per organization, so one tenant's traffic cannot spend another's.
var deviceAgentAuthorizationServerNamespace = uuid.MustParse("6f1c2b8e-4a3d-4e5f-9b7a-1d2c3e4f5a6b")

// deviceAgentAuthorizationServerID is the identifier the device-agent token
// endpoint uses in place of a user session issuer for organizationID.
func deviceAgentAuthorizationServerID(organizationID string) uuid.UUID {
	return uuid.NewSHA1(deviceAgentAuthorizationServerNamespace, []byte(organizationID))
}

// maxPathOrganizationIDLength bounds the organization ID a device-agent token
// path may name before anything is looked up.
const maxPathOrganizationIDLength = 128

// validPathOrganizationID reports whether id can name an organization at all:
// non-empty, bounded, and a single path segment.
func validPathOrganizationID(id string) bool {
	return id != "" && len(id) <= maxPathOrganizationIDLength && !strings.ContainsAny(id, "/?#%")
}

// deviceAgentTokenResponse is the RFC 6749 §5.1 success body.
type deviceAgentTokenResponse struct {
	// AccessToken is the device-agent token.
	AccessToken string `json:"access_token"`

	// TokenType is always "Bearer".
	TokenType string `json:"token_type"`

	// ExpiresIn is the token's lifetime in seconds.
	ExpiresIn int `json:"expires_in"`
}

// HandleDeviceAgentToken exchanges a workload assertion for a device-agent token
// (RFC 7523 §2.1). The tenant is the path's organization, so no other tenant can
// capture a platform subject; every refusal reads the same on the wire.
func (s *Service) HandleDeviceAgentToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	logger := s.logger

	r.Body = http.MaxBytesReader(w, r.Body, tokenFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "failed to parse form")
	}
	grantType := r.PostForm.Get(oauthwire.ParamGrantType)
	if grantType == "" {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "grant_type is required")
	}
	if grantType != oauthwire.GrantTypeJWTBearer {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeUnsupportedGrantType, "grant_type must be "+oauthwire.GrantTypeJWTBearer)
	}
	creds := extractClientCredentials(r)
	if !creds.clientless(r) {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "client authentication is not accepted at this endpoint")
	}
	assertion := r.PostForm.Get(oauthwire.ParamAssertion)
	if assertion == "" {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "assertion is required")
	}
	if s.deviceAgentTokens == nil || s.serverURL == nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeUnsupportedGrantType, "the device-agent token grant is not served by this deployment")
	}
	serverURL := s.serverURL.String()

	presented := presentedWorkload{issuerURL: "", subject: "", issuerID: uuid.Nil}
	token, err := assertioncore.ParseSigned(assertion, workloadAssertionMaxBytes)
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant(string(workload.ReasonMalformed), err))
	}
	var unverified jwt.Claims
	if err := token.UnsafeClaimsWithoutVerification(&unverified); err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant(string(workload.ReasonMalformed), err))
	}
	presented.issuerURL = unverified.Issuer
	presented.subject = unverified.Subject

	// Decided before any organization lookup, so whether the grant is served
	// never depends on, or reveals, which organizations exist.
	if s.workloadGrant == nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeUnsupportedGrantType, "the device-agent token grant is not served by this deployment")
	}

	organizationID := chi.URLParam(r, deviceagenttoken.TokenPathOrganizationParam)
	if !validPathOrganizationID(organizationID) {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("organization_unknown", errors.New("the path names no organization")))
	}
	// A device issuer names the organization its devices enrolled in; a
	// device must not exchange at another organization's endpoint, even one
	// that admitted the same subject string.
	if issuerOrganization, isDeviceIssuer := workloadidentity.DeviceIssuerOrganization(serverURL, unverified.Issuer); isDeviceIssuer && issuerOrganization != organizationID {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("device_issuer_wrong_organization", errors.New("iss is another organization's device issuer")))
	}
	// An organization that does not exist is refused like any other bad
	// assertion, so the endpoint does not reveal which organizations exist.
	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("organization_unknown", err))
		}
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, workloadGrantStageUnavailable("organization_unavailable", err))
	}
	// A disabled organization's tokens are refused on every request, so none
	// is issued; the refusal looks like any other bad assertion.
	if org.DisabledAt.Valid {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("organization_unknown", errors.New("the organization is disabled")))
	}

	tenancy := workloadTenancy{
		OrganizationID:      organizationID,
		ProjectID:           uuid.Nil,
		UserSessionIssuerID: deviceAgentAuthorizationServerID(organizationID),
	}
	presented, err = admitWorkloadAssertion(ctx, s.workloadGrant, tenancy, []string{deviceagenttoken.TokenURL(serverURL, organizationID)}, assertion)
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, err)
	}

	subject := urn.NewWorkloadSubject(presented.issuerID, presented.subject)
	if _, _, err := subject.Workload(); err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("assertion_subject_invalid", err))
	}
	principal := urn.NewWorkloadPrincipal(presented.issuerID, presented.subject)

	ceiling, version, err := deviceagenttoken.EncodeCeiling(organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode device-agent token policy").LogError(ctx, logger)
	}

	// Admitted now as well as on every use, so a workload with no live
	// assigned agent is refused here rather than handed a token every request
	// would refuse, which would have the device exchange again and loop.
	authCtx, err := s.sessionAuthContext(ctx, organizationID, nil, "device-agent-grant:"+uuid.NewString())
	if err != nil {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, workloadGrantStageUnavailable("agent_admission_unavailable", err))
	}
	authorizationCtx := contextvalues.WithAuthenticatedActor(ctx, authCtx, principal)
	_, err = s.admitWorkloadSession(authorizationCtx, organizationID, subject, workloadSessionCredential{DelegatedGrants: ceiling, DelegatedGrantsVersion: version})
	switch {
	case errors.Is(err, errCredentialRejected), err != nil && isCredentialDenial(err):
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("agent_admission_denied", err))
	case err != nil:
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, workloadGrantStageUnavailable("agent_admission_unavailable", err))
	}

	accessToken, _, err := s.deviceAgentTokens.Mint(organizationID, principal, time.Now())
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "mint device-agent token").LogError(ctx, logger)
	}
	body, err := json.Marshal(deviceAgentTokenResponse{
		AccessToken: accessToken,
		TokenType:   oauthwire.TokenTypeBearer,
		ExpiresIn:   int(deviceagenttoken.Lifetime / time.Second),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, fmt.Errorf("marshal device-agent token response: %w", err), "write token response").LogError(ctx, logger)
	}
	if err := writeTokenSuccess(ctx, w, logger, body); err != nil {
		return err
	}
	logger.InfoContext(ctx, "device-agent token issued",
		attr.SlogOAuthGrant(oauthwire.GrantTypeJWTBearer),
		attr.SlogOrganizationID(organizationID),
		attr.SlogWorkloadIssuerID(presented.issuerID.String()),
		attr.SlogWorkloadSubject(presented.subject),
	)
	return nil
}
