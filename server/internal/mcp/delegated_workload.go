package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

const (
	tokenTypeJWT         = "urn:ietf:params:oauth:token-type:jwt"          //nolint:gosec // standardized public token type identifier, not a credential
	tokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token" //nolint:gosec // standardized public token type identifier, not a credential
	paramActorToken      = "actor_token"
	paramActorTokenType  = "actor_token_type"
)

// isDelegatedWorkloadExchange reports an RFC 8693 exchange in which a workload
// acts for the subject of a delegation the platform minted for it.
func isDelegatedWorkloadExchange(r *http.Request) bool {
	return r.PostForm.Get(oauthwire.ParamGrantType) == oauthwire.GrantTypeTokenExchange &&
		r.PostForm.Get(paramActorToken) != "" &&
		extractClientCredentials(r).clientless(r)
}

// handleDelegatedWorkloadExchange serves an assistant runtime that acts for the
// person in its current turn. The actor token is the runtime's workload
// assertion and is admitted exactly as the workload grant admits it. The
// subject token is a delegation the platform minted for that workload alone.
// The result is the turn's agent credential, bounded at admission by the agent,
// its owner and the person.
func (s *Service) handleDelegatedWorkloadExchange(ctx context.Context, w http.ResponseWriter, r *http.Request, issuer *sharedIssuer) error {
	logger := issuer.logger
	if s.assistantTokens == nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeUnsupportedGrantType, "unsupported grant_type")
	}
	if refused, err := s.refuseUnservedWorkloadGrant(ctx, w, r, extractClientCredentials(r), logger); refused {
		return err
	}
	form := r.PostForm
	for _, name := range []string{oauthwire.ParamSubjectTokenType, paramActorTokenType} {
		if len(form[name]) != 1 || form.Get(name) != tokenTypeJWT {
			return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, name+" must be "+tokenTypeJWT)
		}
	}
	if requested := form.Get(oauthwire.ParamRequestedTokenType); requested != "" && requested != tokenTypeAccessToken {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "only an access token can be issued")
	}
	if len(form[oauthwire.ParamSubjectToken]) != 1 || len(form[paramActorToken]) != 1 || form.Get(oauthwire.ParamSubjectToken) == "" {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidRequest, "exactly one subject_token and actor_token are required")
	}

	// The delegation names its project; the actor is admitted for that project.
	delegated, mayAct, err := s.principalCredentials.ValidateDelegation(form.Get(oauthwire.ParamSubjectToken))
	if err != nil {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "subject_token is invalid")
	}
	shared := issuer.authorizationServer
	credential := delegated.Credential
	if credential.OrganizationID != issuer.organizationID || (shared.projectID != uuid.Nil && shared.projectID != credential.ProjectID) {
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "subject_token is outside this authorization server")
	}
	urls, err := shared.urls()
	if err != nil {
		return fmt.Errorf("exchange delegated workload: %w", err)
	}
	presented, err := admitWorkloadAssertion(ctx, s.workloadGrant, workloadTenancy{OrganizationID: issuer.organizationID, ProjectID: credential.ProjectID, UserSessionIssuerID: shared.issuerID}, []string{urls.Issuer, urls.Token}, form.Get(paramActorToken))
	if err != nil && !presented.signatureVerified {
		return s.refuseUnverifiedWorkloadGrant(ctx, w, logger, presented, err)
	}
	t := s.trackSharedWorkloadGrant(r, issuer, presented, urn.NewWorkloadSubject(presented.issuerID, presented.subject))
	defer t.Finish(ctx)
	if err != nil {
		refuseWorkloadGrantRow(ctx, t, err)
		return s.writeWorkloadGrantRefusal(ctx, w, logger, presented, err)
	}

	actor := urn.NewWorkloadPrincipal(presented.issuerID, presented.subject)
	if mayAct.String() != actor.String() {
		t.Refuse(ctx, mcpconnectionattempts.CodeAssertionInvalid, mcpconnectionattempts.Detail("delegation_actor_mismatch"))
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "actor may not exchange this subject_token")
	}
	// The workload must still act for the delegated agent.
	agentID, assigned, err := workloadidentity.ResolveAssignedAgent(ctx, s.db, workloadidentity.AssignmentParams{OrganizationID: issuer.organizationID, WorkloadIssuerID: presented.issuerID, Subject: presented.subject})
	if err != nil {
		t.Refuse(ctx, mcpconnectionattempts.CodeInternalError)
		return fmt.Errorf("exchange delegated workload: %w", err)
	}
	if !assigned || agentID.String() != credential.Principal.ID {
		t.Refuse(ctx, mcpconnectionattempts.CodeAssertionInvalid, mcpconnectionattempts.Detail("delegation_agent_mismatch"))
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "actor does not act for the delegated agent")
	}

	token, err := s.assistantTokens.ExchangeDelegation(ctx, form.Get(oauthwire.ParamSubjectToken), actor)
	if errors.Is(err, principalcredential.ErrInvalid) {
		t.Refuse(ctx, mcpconnectionattempts.CodeAssertionInvalid, mcpconnectionattempts.Detail("delegation_unbound"))
		return writeTokenError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidGrant, "subject_token is invalid")
	}
	if err != nil {
		t.Refuse(ctx, mcpconnectionattempts.CodeInternalError)
		return fmt.Errorf("exchange delegated workload: %w", err)
	}
	body, err := json.Marshal(map[string]any{
		"access_token":      token,
		"token_type":        oauthwire.TokenTypeBearer,
		"issued_token_type": tokenTypeAccessToken,
		"expires_in":        int(principalcredential.Lifetime.Seconds()),
	})
	if err != nil {
		t.Refuse(ctx, mcpconnectionattempts.CodeInternalError)
		return fmt.Errorf("encode delegated workload token: %w", err)
	}
	return writeTokenSuccess(ctx, w, logger, body)
}
