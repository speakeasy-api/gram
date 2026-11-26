package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	delegatedTokenTypeJWT         = "urn:ietf:params:oauth:token-type:jwt"
	delegatedTokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token"
)

// delegatedWorkloadFixture is a workload fixture on a shared authorization
// server, with an assistant whose turns the workload's agent runs.
type delegatedWorkloadFixture struct {
	workloadGrantFixture
	authCtx     *contextvalues.AuthContext
	issuerURL   string
	assistantID uuid.UUID
}

func newDelegatedWorkloadFixture(t *testing.T) delegatedWorkloadFixture {
	t.Helper()

	f := newWorkloadGrantFixture(t)
	issuerURL := f.sharedIssuer(t)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.Equal(t, f.fx.target.ProjectID, *authCtx.ProjectID)
	require.Equal(t, f.fx.userID, authCtx.UserID)
	grantProjectRead(t, f.ti, authCtx)
	return delegatedWorkloadFixture{
		workloadGrantFixture: f,
		authCtx:              authCtx,
		issuerURL:            issuerURL,
		assistantID:          createAssistant(t, f.ti, authCtx, "Delegated runtime"),
	}
}

// actor is the workload principal the fixture's assertion presents.
func (f delegatedWorkloadFixture) actor() urn.Principal {
	return urn.NewWorkloadPrincipal(f.issuerID, f.subject)
}

// delegation mints a turn credential for agentID acting for the fixture's user
// and delegates it to actor.
func (f delegatedWorkloadFixture) delegation(t *testing.T, agentID uuid.UUID, actor urn.Principal) string {
	t.Helper()

	turn := mintAgentTurnCredential(t, f.ti, f.authCtx, f.assistantID, agentID)
	raw, err := f.ti.assistantTokens.DelegateTurnCredential(t.Context(), turn, actor)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.False(t, principalcredential.IsToken(raw), "a delegation must not authenticate by itself")
	return raw
}

func (f delegatedWorkloadFixture) exchangeForm(t *testing.T, subjectToken string) url.Values {
	t.Helper()

	return url.Values{
		oauthwire.ParamGrantType:        {oauthwire.GrantTypeTokenExchange},
		oauthwire.ParamSubjectToken:     {subjectToken},
		oauthwire.ParamSubjectTokenType: {delegatedTokenTypeJWT},
		"actor_token":                   {f.assertion(t, f.issuerURL)},
		"actor_token_type":              {delegatedTokenTypeJWT},
	}
}

func (f delegatedWorkloadFixture) post(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	return sharedToken(t, t.Context(), f.ti, f.fx.target.UserSessionIssuerID.String(), form)
}

func requireTokenError(t *testing.T, w *httptest.ResponseRecorder, code, description string) {
	t.Helper()

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	require.Equal(t, code, body["error"], w.Body.String())
	require.Equal(t, description, body["error_description"], w.Body.String())
}

func TestDelegatedWorkloadExchange_IssuesTurnCredentialForUser(t *testing.T) {
	t.Parallel()

	f := newDelegatedWorkloadFixture(t)
	w := f.post(t, f.exchangeForm(t, f.delegation(t, f.agentID, f.actor())))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, oauthwire.TokenTypeBearer, resp["token_type"])
	require.Equal(t, delegatedTokenTypeAccessToken, resp["issued_token_type"])
	require.InDelta(t, principalcredential.Lifetime.Seconds(), resp["expires_in"], 0)
	require.NotContains(t, resp, "refresh_token")

	token, ok := resp["access_token"].(string)
	require.True(t, ok)
	require.True(t, principalcredential.IsToken(token), "the exchange issues a principal credential")
	validated, err := f.ti.principalCredentials.Validate(token)
	require.NoError(t, err)
	require.Equal(t, f.fx.userID, validated.Credential.AuthorizerUserID)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()).String(), validated.Credential.Principal.String())

	_, claims, err := f.ti.assistantTokens.AuthorizeRuntime(t.Context(), "Bearer "+token)
	require.NoError(t, err, "the issued credential must authorize the assistant runtime")
	require.Equal(t, f.fx.userID, claims.UserID)
}

func TestDelegatedWorkloadExchange_RefusesDelegationForAnotherActor(t *testing.T) {
	t.Parallel()

	f := newDelegatedWorkloadFixture(t)
	other := urn.NewWorkloadPrincipal(f.issuerID, f.subject+":other")
	requireTokenError(t, f.post(t, f.exchangeForm(t, f.delegation(t, f.agentID, other))), "invalid_grant", "actor may not exchange this subject_token")
}

func TestDelegatedWorkloadExchange_RefusesWorkloadNotAssignedToAgent(t *testing.T) {
	t.Parallel()

	f := newDelegatedWorkloadFixture(t)
	otherAgent := createConsentAgent(t, f.ctx, f.ti, f.fx, "Unassigned agent")
	requireTokenError(t, f.post(t, f.exchangeForm(t, f.delegation(t, otherAgent.ID, f.actor()))), "invalid_grant", "actor does not act for the delegated agent")
}

func TestDelegatedWorkloadExchange_RequiresActorToken(t *testing.T) {
	t.Parallel()

	f := newDelegatedWorkloadFixture(t)
	form := f.exchangeForm(t, f.delegation(t, f.agentID, f.actor()))
	form.Del("actor_token")
	form.Del("actor_token_type")
	requireTokenError(t, f.post(t, form), "unsupported_grant_type", "unsupported grant_type")
}

func TestDelegatedWorkloadExchange_RejectsWrongSubjectTokenType(t *testing.T) {
	t.Parallel()

	f := newDelegatedWorkloadFixture(t)
	form := f.exchangeForm(t, f.delegation(t, f.agentID, f.actor()))
	form.Set(oauthwire.ParamSubjectTokenType, delegatedTokenTypeAccessToken)
	requireTokenError(t, f.post(t, form), "invalid_request", "subject_token_type must be "+delegatedTokenTypeJWT)
}

func TestDelegatedWorkloadExchange_RefusesDelegationFromAnotherOrganization(t *testing.T) {
	t.Parallel()

	f := newDelegatedWorkloadFixture(t)
	raw, _, err := f.ti.principalCredentials.MintDelegation(principalcredential.Credential{
		OrganizationID:            "another-org",
		ProjectID:                 f.fx.target.ProjectID,
		Principal:                 urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()),
		AuthorizerUserID:          f.fx.userID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, f.actor())
	require.NoError(t, err)
	requireTokenError(t, f.post(t, f.exchangeForm(t, raw)), "invalid_grant", "subject_token is outside this authorization server")
}

func TestDelegatedWorkloadExchange_RefusesDelegationNotBoundToAThread(t *testing.T) {
	t.Parallel()

	// Signed for the right actor and agent, but never recorded against an
	// assistant thread, as when its binding expired or it was not platform-issued.
	f := newDelegatedWorkloadFixture(t)
	raw, _, err := f.ti.principalCredentials.MintDelegation(principalcredential.Credential{
		OrganizationID:            f.authCtx.ActiveOrganizationID,
		ProjectID:                 f.fx.target.ProjectID,
		Principal:                 urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()),
		AuthorizerUserID:          f.fx.userID,
		SelectionProfileVersionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}, f.actor())
	require.NoError(t, err)
	_, _, err = f.ti.principalCredentials.ValidateDelegation(raw)
	require.NoError(t, err, "the delegation itself is valid; only its thread binding is missing")
	_, err = f.ti.assistantTokens.ExchangeDelegation(t.Context(), raw, f.actor())
	require.ErrorIs(t, err, principalcredential.ErrInvalid)
	requireTokenError(t, f.post(t, f.exchangeForm(t, raw)), "invalid_grant", "subject_token is invalid")
}
