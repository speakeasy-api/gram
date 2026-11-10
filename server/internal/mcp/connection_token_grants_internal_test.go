package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestWorkloadGrantHappened(t *testing.T) {
	t.Parallel()

	cause := errors.New("cause")
	for _, tc := range []struct {
		name   string
		err    error
		want   mcpconnectionattempts.Code
		detail string
	}{
		{name: "subject invalid", err: refuseWorkloadGrant("assertion_subject_invalid", cause), want: mcpconnectionattempts.CodeAssertionInvalid, detail: "assertion_subject_invalid"},
		{name: "issuer trust removed", err: refuseWorkloadGrant("issuer_untrusted", cause), want: mcpconnectionattempts.CodeIssuerNotTrusted, detail: "issuer_untrusted"},
		{name: "expired after signature", err: refuseWorkloadGrant("assertion_expired", cause), want: mcpconnectionattempts.CodeAssertionInvalid, detail: "assertion_expired"},
		{name: "subject not admitted", err: refuseWorkloadGrant("subject_not_admitted", cause), want: mcpconnectionattempts.CodeSubjectNotAdmitted, detail: "subject_not_admitted"},
		{name: "endpoint not agent addressable", err: refuseWorkloadGrant("endpoint_not_agent_addressable", cause), want: mcpconnectionattempts.CodeEndpointNotAgentAddressable, detail: "endpoint_not_agent_addressable"},
		{name: "agent admission denied", err: refuseWorkloadGrant("agent_admission_denied", cause), want: mcpconnectionattempts.CodeNoServerAccess, detail: "agent_admission_denied"},
		{name: "issuer policy changed", err: refuseWorkloadGrant("issuer_policy_changed", cause), want: mcpconnectionattempts.CodeTransientFailure, detail: "issuer_policy_changed"},
		{name: "unnamed refusal", err: refuseWorkloadGrant("policy", cause), want: mcpconnectionattempts.CodeAssertionOther, detail: "policy"},
		{name: "stage unavailable", err: workloadGrantStageUnavailable("session_persist_unavailable", cause), want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "session_persist_unavailable"},
		{name: "rate limited", err: &workloadGrantError{reason: "issuer_lookup_rate_limited", outcome: workloadGrantRateLimited, retryAfter: time.Second, err: cause}, want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "issuer_lookup_rate_limited"},
		{name: "choices not accepted", err: oops.NewOAuthError(http.StatusBadRequest, "invalid_request", "no", errChoiceNotAccepted), want: mcpconnectionattempts.CodeUpstreamCredentialChoice, detail: "choice_not_accepted"},
		{name: "server not reached", err: oops.NewOAuthError(http.StatusBadRequest, "invalid_request", "no", errChoiceServerNotReached), want: mcpconnectionattempts.CodeCredentialChoiceInvalid, detail: "choice_server_not_reached"},
		{name: "credential not needed", err: setupRequired("no", errChoiceTakesNoCredential), want: mcpconnectionattempts.CodeCredentialChoiceInvalid, detail: "credential_not_needed"},
		{name: "credential needed", err: setupRequired("no", errChoiceNeedsCredential), want: mcpconnectionattempts.CodeCredentialChoiceInvalid, detail: "credential_needed"},
		{name: "sign-in not set up", err: setupRequired("no", errChoiceNotSetUp), want: mcpconnectionattempts.CodeUpstreamSignInNotSetUp, detail: "sign_in_not_set_up"},
		{name: "credential not ready", err: setupRequired("no", errChoiceNotReady), want: mcpconnectionattempts.CodeUpstreamSignInNotSetUp, detail: "credential_not_ready"},
		{name: "no credential ready", err: setupRequired("no", errChoiceNoneReady), want: mcpconnectionattempts.CodeUpstreamSignInNotSetUp, detail: "no_credential_ready"},
		{name: "credential denied", err: accessDenied("no", errChoiceDenied), want: mcpconnectionattempts.CodeUpstreamCredentialDenied, detail: "credential_denied"},
		{name: "no credential allowed", err: accessDenied("no", errChoiceNoneAllowed), want: mcpconnectionattempts.CodeUpstreamCredentialDenied, detail: "no_credential_allowed"},
		{name: "credential not chosen", err: oops.NewOAuthError(http.StatusBadRequest, tokenErrorSelectionRequired, "name one", errChoiceNotNamed), want: mcpconnectionattempts.CodeUpstreamCredentialNotChosen, detail: "credential_not_chosen"},
		{name: "unexpected", err: cause, want: mcpconnectionattempts.CodeInternalError, detail: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			refuseWorkloadGrantRow(t.Context(), rec.Recorder.Track(directScope(), mcpconnectionattempts.StepInboundToken), tc.err)
			require.Equal(t, tc.detail, rec.RequireEnded(mcpconnectionattempts.StepInboundToken, tc.want).SubReason)
		})
	}
}

func directScope() mcpconnectionattempts.Scope {
	return connectionScope(directEndpoint(), mcpconnectionattempts.AppWorkload)
}

// assistantGrantCase posts a client credentials grant to a server endpoint.
type assistantGrantCase struct {
	service  *Service
	endpoint *ResolvedMcpEndpoint
	client   *usersessions_repo.UserSessionClient
}

func newAssistantGrantCase(t *testing.T, recorder *mcpconnectionattempts.Recorder, issue func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error)) assistantGrantCase {
	t.Helper()

	s, endpoint, _ := newTokenDispatchTestService(t)
	endpoint.McpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	s.SetConnectionRecorder(recorder)
	s.assistantWorkloadTokens = assistantWorkloadIssuerFunc(issue)
	client := &usersessions_repo.UserSessionClient{ID: uuid.New(), ClientID: "assistant-client", UserSessionIssuerID: endpoint.UserSessionIssuerID, ClientSecretHash: pgtype.Text{String: "hashed", Valid: true}}
	return assistantGrantCase{service: s, endpoint: endpoint, client: client}
}

func (c assistantGrantCase) post(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	r := newTokenDispatchRequest(t, form)
	w := httptest.NewRecorder()
	require.NoError(t, c.service.handleAssistantClientCredentialsGrant(r.Context(), w, r, c.endpoint, c.client, "https://gram.example", "client_secret_post", c.service.logger))
	return w
}

var assistantGrantForm = url.Values{"resource": {"https://gram.example/mcp/test"}, "scope": {"mcp:connect"}}

func TestAssistantClientCredentialsRecordsSuccess(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	agentID := uuid.New()
	c := newAssistantGrantCase(t, rec.Recorder, func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
		return AssistantWorkloadToken{AssistantID: uuid.New(), AgentID: agentID, AccessToken: "opaque-token", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})

	w := c.post(t, assistantGrantForm)
	require.Equal(t, http.StatusOK, w.Code)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultSignedIn)
	require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
	require.Equal(t, oauthwire.GrantTypeClientCredentials, row.GrantType)
	require.Equal(t, urn.NewAgentSubject(agentID).String(), row.SubjectURN)
	require.Equal(t, c.client.ID.String(), row.UserSessionClientID)
	require.Equal(t, mcpconnectionattempts.AppOf(c.client.ClientID).String(), row.AppKey)
	require.Len(t, rec.Rows(), 1)
}

func TestAssistantClientCredentialsRecordsFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		form     url.Values
		public   bool
		issueErr error
		resolved bool
		status   int
		want     mcpconnectionattempts.Code
		detail   string
	}{
		{name: "public client", form: assistantGrantForm, public: true, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeClientAuthFailed, detail: "client_not_confidential"},
		{name: "wrong resource", form: url.Values{"resource": {"https://other.example/mcp/test"}, "scope": {"mcp:connect"}}, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeResourceMismatch},
		{name: "unsupported scope", form: url.Values{"resource": {"https://gram.example/mcp/test"}, "scope": {"tools:read"}}, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeInvalidRequest, detail: "invalid_scope"},
		{name: "no assistant", form: assistantGrantForm, issueErr: pgx.ErrNoRows, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeSubjectNotAdmitted, detail: "assistant_not_found"},
		{name: "invalid identity", form: assistantGrantForm, issueErr: assistantidentity.ErrInvalidIdentity, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeSubjectNotAdmitted, detail: "assistant_identity_invalid"},
		{name: "refused", form: assistantGrantForm, issueErr: refuseWorkloadGrant("agent_admission_denied", errors.New("denied")), resolved: true, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeNoServerAccess, detail: "agent_admission_denied"},
		{name: "rate limited", form: assistantGrantForm, issueErr: &workloadGrantError{reason: "issuer_lookup_rate_limited", outcome: workloadGrantRateLimited, retryAfter: time.Second, err: errors.New("busy")}, resolved: true, status: http.StatusTooManyRequests, want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "issuer_lookup_rate_limited"},
		{name: "stage unavailable", form: assistantGrantForm, issueErr: workloadGrantStageUnavailable("session_persist_unavailable", errors.New("down")), resolved: true, status: http.StatusServiceUnavailable, want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "session_persist_unavailable"},
		{name: "issuance unavailable", form: assistantGrantForm, issueErr: errors.New("database unavailable"), resolved: true, status: http.StatusServiceUnavailable, want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "issuance_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			agentID := uuid.New()
			c := newAssistantGrantCase(t, rec.Recorder, func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
				if tc.resolved {
					return AssistantWorkloadToken{AssistantID: uuid.New(), AgentID: agentID}, tc.issueErr
				}
				return AssistantWorkloadToken{}, tc.issueErr
			})
			c.client.ClientSecretHash.Valid = !tc.public

			w := c.post(t, tc.form)
			require.Equal(t, tc.status, w.Code)

			row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, tc.want)
			require.Equal(t, tc.detail, row.SubReason)
			require.Equal(t, oauthwire.GrantTypeClientCredentials, row.GrantType)
			if tc.resolved {
				require.Equal(t, urn.NewAgentSubject(agentID).String(), row.SubjectURN)
			} else {
				require.Empty(t, row.SubjectURN, "nothing resolved the agent")
			}
			require.Len(t, rec.Rows(), 1)
		})
	}
}

func TestAssistantClientCredentialsInvalidIssuanceRecordsInternalError(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	c := newAssistantGrantCase(t, rec.Recorder, func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
		return AssistantWorkloadToken{AssistantID: uuid.New(), AccessToken: "opaque-token", ExpiresAt: time.Now().Add(time.Hour)}, nil
	})

	w := c.post(t, assistantGrantForm)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, "issued_token_invalid", rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeInternalError).SubReason)
}

func TestAssistantClientCredentialsLogsOffRecordsNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.NewLogsOff(t)
	c := newAssistantGrantCase(t, rec.Recorder, func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
		return AssistantWorkloadToken{AssistantID: uuid.New(), AgentID: uuid.New(), AccessToken: "opaque-token", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})

	require.Equal(t, http.StatusOK, c.post(t, assistantGrantForm).Code)
	require.Equal(t, http.StatusBadRequest, c.post(t, url.Values{"scope": {"mcp:connect"}}).Code)
	rec.RequireNothing()
}

func TestAssistantClientCredentialsOrganizationGatewayRecordsUnacceptedMethod(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	c := newAssistantGrantCase(t, rec.Recorder, func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
		return AssistantWorkloadToken{}, errors.New("unreachable")
	})
	c.endpoint.McpServerID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	c.endpoint.ProjectID = uuid.Nil
	c.endpoint.MetaMcpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	c.endpoint.RouteBase = mcproute.OrganizationGateway

	require.Equal(t, http.StatusBadRequest, c.post(t, assistantGrantForm).Code)
	require.Equal(t, "organization_gateway", rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeSignInMethodNotAccepted).SubReason)
}

func TestAssistantClientCredentialsTokenWithoutAgentRecordsOurError(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	c := newAssistantGrantCase(t, rec.Recorder, func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
		return AssistantWorkloadToken{AssistantID: uuid.New(), AccessToken: "opaque-token", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})

	require.Equal(t, http.StatusInternalServerError, c.post(t, assistantGrantForm).Code, "a token naming no agent is never written")
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeInternalError)
	require.Equal(t, "issued_token_invalid", row.SubReason)
	require.Len(t, rec.Rows(), 1)
}

// Trust removed after verification reaches the issuance transaction as an untrusted issuer, which an admin fixes.
func TestWorkloadIssuerTrustRemovedBeforeIssuance(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_workload_trust_removed")
	require.NoError(t, err)
	tx, err := pool.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the SQLc issuer lock under test
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	presented := presentedWorkload{issuerURL: "https://issuer.example", subject: "repo:acme/app", issuerID: uuid.New(), assertionPolicyChecked: true, signatureVerified: true}
	err = lockWorkloadAssertionPolicy(t.Context(), tx, "org-test", nil, presented)

	rec := mcpconnectionattemptstest.New(t)
	refuseWorkloadGrantRow(t.Context(), rec.Recorder.Track(directScope(), mcpconnectionattempts.StepInboundToken), err)
	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.CodeIssuerNotTrusted)
	require.Equal(t, "issuer_untrusted", row.SubReason)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
}
