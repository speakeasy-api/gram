// The workload assertion grant at the device-agent token endpoint: a headless
// device exchanges its identity assertion for a bearer token it presents to
// the device-agent API (plugin policy and hooks) instead of an agent API key.

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// deviceAgentAuthorizationServerNamespace derives the identifier each
// organization's device-agent token endpoint charges its issuer lookups, key
// fetches, and replay records to. One endpoint serves every organization, so
// keying those budgets on it alone would let one organization's traffic spend
// another's.
var deviceAgentAuthorizationServerNamespace = uuid.MustParse("6f1c2b8e-4a3d-4e5f-9b7a-1d2c3e4f5a6b")

// deviceAgentAuthorizationServerID is the identifier the device-agent token
// endpoint uses in place of a user session issuer for organizationID.
func deviceAgentAuthorizationServerID(organizationID string) uuid.UUID {
	return uuid.NewSHA1(deviceAgentAuthorizationServerNamespace, []byte(organizationID))
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

// HandleDeviceAgentToken exchanges a workload's identity assertion for a
// device-agent token (RFC 7523 §2.1, clientless).
//
// One endpoint serves every organization, so the tenant comes from the
// assertion's iss, which must be an organization's Speakeasy device issuer:
// that URL names its organization, and the issuer row is then looked up in
// that organization alone. A platform issuer's URL names no organization, and
// one organization admitting another's platform subject must never decide
// where that subject's assertions land, so such assertions are refused here.
//
// Past that, the assertion is verified and its subject admitted exactly as at
// an MCP token endpoint, with the audience this endpoint's URL, and the
// workload must have a live assigned agent. Refusals all read the same on the
// wire.
func (s *Service) HandleDeviceAgentToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	logger := s.logger

	r.Body = http.MaxBytesReader(w, r.Body, tokenFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "failed to parse form")
	}
	if grantType := r.PostForm.Get(oauthwire.ParamGrantType); grantType != oauthwire.GrantTypeJWTBearer {
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
	organizationID, ok := workloadidentity.DeviceIssuerOrganization(serverURL, unverified.Issuer)
	if !ok {
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, refuseWorkloadGrant("issuer_not_device_issuer", errors.New("iss is not a Speakeasy device issuer")))
	}

	if refused, err := s.refuseUnservedWorkloadGrant(ctx, w, r, creds, organizationID, logger); refused {
		return err
	}

	tenancy := workloadTenancy{
		OrganizationID:      organizationID,
		ProjectID:           uuid.Nil,
		UserSessionIssuerID: deviceAgentAuthorizationServerID(organizationID),
	}
	presented, err = admitWorkloadAssertion(ctx, s.workloadGrant, tenancy, []string{deviceagenttoken.TokenURL(serverURL)}, assertion)
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
	case errors.Is(err, errWorkloadRolloutDisabled), errors.Is(err, errCredentialRejected), err != nil && isCredentialDenial(err):
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
