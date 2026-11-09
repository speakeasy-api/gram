package mcp

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
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
		{name: "malformed", err: refuseWorkloadGrant(string(workload.ReasonMalformed), cause), want: mcpconnectionattempts.CodeAssertionInvalid, detail: "assertion_malformed"},
		{name: "replayed", err: refuseWorkloadGrant(string(workload.ReasonReplayed), cause), want: mcpconnectionattempts.CodeAssertionInvalid, detail: "assertion_replayed"},
		{name: "device key mismatch", err: refuseWorkloadGrant("device_key_id_mismatch", cause), want: mcpconnectionattempts.CodeAssertionInvalid, detail: "device_key_id_mismatch"},
		{name: "untrusted issuer", err: refuseWorkloadGrant("issuer_untrusted", cause), want: mcpconnectionattempts.CodeIssuerNotTrusted, detail: "issuer_untrusted"},
		{name: "invalid key set address", err: refuseWorkloadGrant("issuer_jwks_uri_invalid", cause), want: mcpconnectionattempts.CodeIssuerNotTrusted, detail: "issuer_jwks_uri_invalid"},
		{name: "unknown key source", err: refuseWorkloadGrant("issuer_key_source_unknown", cause), want: mcpconnectionattempts.CodeAssertionVerifierMisconfigured, detail: "issuer_key_source_unknown"},
		{name: "subject not admitted", err: refuseWorkloadGrant("subject_not_admitted", cause), want: mcpconnectionattempts.CodeSubjectNotAdmitted, detail: "subject_not_admitted"},
		{name: "device key not registered", err: refuseWorkloadGrant("device_key_not_registered", cause), want: mcpconnectionattempts.CodeSubjectNotAdmitted, detail: "device_key_not_registered"},
		{name: "endpoint not agent addressable", err: refuseWorkloadGrant("endpoint_not_agent_addressable", cause), want: mcpconnectionattempts.CodeEndpointNotAgentAddressable, detail: "endpoint_not_agent_addressable"},
		{name: "agent admission denied", err: refuseWorkloadGrant("agent_admission_denied", cause), want: mcpconnectionattempts.CodeNoServerAccess, detail: "agent_admission_denied"},
		{name: "issuer policy changed", err: refuseWorkloadGrant("issuer_policy_changed", cause), want: mcpconnectionattempts.CodeConnectionChanged, detail: "issuer_policy_changed"},
		{name: "unnamed refusal", err: refuseWorkloadGrant("policy", cause), want: mcpconnectionattempts.CodeAssertionOther, detail: "policy"},
		{name: "issuer keys unreachable", err: workloadGrantStageUnavailable(string(workload.ReasonKeyUnresolvable), cause), want: mcpconnectionattempts.CodeIDPTransientFailure, detail: "assertion_key_unresolvable"},
		{name: "stage unavailable", err: workloadGrantStageUnavailable("session_persist_unavailable", cause), want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "session_persist_unavailable"},
		{name: "rate limited", err: &workloadGrantError{reason: "issuer_lookup_rate_limited", outcome: workloadGrantRateLimited, retryAfter: time.Second, err: cause}, want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "issuer_lookup_rate_limited"},
		{name: "upstream setup missing", err: setupRequired("not set up"), want: mcpconnectionattempts.CodeUpstreamSignInNotSetUp, detail: tokenErrorSetupRequired},
		{name: "upstream access denied", err: accessDenied("no"), want: mcpconnectionattempts.CodeNoServerAccess, detail: "access_denied"},
		{name: "upstream choice refused", err: oops.NewOAuthError(http.StatusBadRequest, "invalid_request", "bad choice", nil), want: mcpconnectionattempts.CodeUpstreamCredentialChoice, detail: "invalid_request"},
		{name: "verifier misconfigured", err: fmt.Errorf("verify: %w", &workload.Error{Reason: workload.ReasonVerifierMisconfigured}), want: mcpconnectionattempts.CodeAssertionVerifierMisconfigured, detail: "assertion_verifier_misconfigured"},
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
	assistantID := uuid.New()
	c := newAssistantGrantCase(t, rec.Recorder, func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
		return AssistantWorkloadToken{AssistantID: assistantID, AccessToken: "opaque-token", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})

	w := c.post(t, assistantGrantForm)
	require.Equal(t, http.StatusOK, w.Code)

	row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, mcpconnectionattempts.ResultSignedIn)
	require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
	require.Equal(t, oauthwire.GrantTypeClientCredentials, row.GrantType)
	require.Equal(t, urn.NewAgentSubject(assistantID).String(), row.SubjectURN)
	require.Equal(t, c.client.ID.String(), row.UserSessionClientID)
	require.Equal(t, mcpconnectionattempts.AppWorkload.String(), row.AppKey)
	require.Len(t, rec.Rows(), 1)
}

func TestAssistantClientCredentialsRecordsFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		form     url.Values
		public   bool
		issueErr error
		status   int
		want     mcpconnectionattempts.Code
		detail   string
	}{
		{name: "public client", form: assistantGrantForm, public: true, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeClientAuthFailed, detail: "client_not_confidential"},
		{name: "wrong resource", form: url.Values{"resource": {"https://other.example/mcp/test"}, "scope": {"mcp:connect"}}, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeResourceMismatch},
		{name: "unsupported scope", form: url.Values{"resource": {"https://gram.example/mcp/test"}, "scope": {"tools:read"}}, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeInvalidRequest, detail: "invalid_scope"},
		{name: "no assistant", form: assistantGrantForm, issueErr: pgx.ErrNoRows, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeSubjectNotAdmitted, detail: "assistant_not_found"},
		{name: "invalid identity", form: assistantGrantForm, issueErr: assistantidentity.ErrInvalidIdentity, status: http.StatusBadRequest, want: mcpconnectionattempts.CodeSubjectNotAdmitted, detail: "assistant_identity_invalid"},
		{name: "refused", form: assistantGrantForm, issueErr: refuseWorkloadGrant("agent_admission_denied", errors.New("denied")), status: http.StatusBadRequest, want: mcpconnectionattempts.CodeNoServerAccess, detail: "agent_admission_denied"},
		{name: "rate limited", form: assistantGrantForm, issueErr: &workloadGrantError{reason: "issuer_lookup_rate_limited", outcome: workloadGrantRateLimited, retryAfter: time.Second, err: errors.New("busy")}, status: http.StatusTooManyRequests, want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "issuer_lookup_rate_limited"},
		{name: "stage unavailable", form: assistantGrantForm, issueErr: workloadGrantStageUnavailable("session_persist_unavailable", errors.New("down")), status: http.StatusServiceUnavailable, want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "session_persist_unavailable"},
		{name: "issuance unavailable", form: assistantGrantForm, issueErr: errors.New("database unavailable"), status: http.StatusServiceUnavailable, want: mcpconnectionattempts.CodeAssertionCheckUnavailable, detail: "issuance_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := mcpconnectionattemptstest.New(t)
			c := newAssistantGrantCase(t, rec.Recorder, func(context.Context, AssistantWorkloadTokenRequest) (AssistantWorkloadToken, error) {
				if tc.issueErr != nil {
					return AssistantWorkloadToken{}, tc.issueErr
				}
				return AssistantWorkloadToken{AssistantID: uuid.New(), AccessToken: "opaque-token", ExpiresAt: time.Now().Add(time.Minute)}, nil
			})
			c.client.ClientSecretHash.Valid = !tc.public

			w := c.post(t, tc.form)
			require.Equal(t, tc.status, w.Code)

			row := rec.RequireEnded(mcpconnectionattempts.StepInboundToken, tc.want)
			require.Equal(t, tc.detail, row.SubReason)
			require.Equal(t, oauthwire.GrantTypeClientCredentials, row.GrantType)
			require.Empty(t, row.SubjectURN)
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
		return AssistantWorkloadToken{AssistantID: uuid.New(), AccessToken: "opaque-token", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})

	require.Equal(t, http.StatusOK, c.post(t, assistantGrantForm).Code)
	require.Equal(t, http.StatusBadRequest, c.post(t, url.Values{"scope": {"mcp:connect"}}).Code)
	rec.RequireNothing()
}
