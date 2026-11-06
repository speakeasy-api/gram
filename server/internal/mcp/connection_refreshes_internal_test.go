package mcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestUnconfirmedRefreshCheck(t *testing.T) {
	t.Parallel()

	for _, reason := range []identitychaining.Reason{identitychaining.ReasonTransientFailure, identitychaining.ReasonProviderUnavailable, identitychaining.ReasonUnknownRejection} {
		require.Equal(t, upstreamRefreshCheckFailed, unconfirmedRefreshCheck(reason), reason)
	}
	for _, reason := range []identitychaining.Reason{
		identitychaining.ReasonConfigurationRequired,
		identitychaining.ReasonStaleConfiguration,
		identitychaining.ReasonBindingNotReady,
		identitychaining.ReasonInvalidTarget,
		identitychaining.ReasonScopePolicyDenied,
		identitychaining.ReasonAccessDenied,
		identitychaining.ReasonInvalidClient,
		identitychaining.ReasonMalformedAssertion,
		identitychaining.ReasonInsufficientScope,
		identitychaining.ReasonIneligibleIdentity,
	} {
		require.Equal(t, upstreamRefreshUnverified, unconfirmedRefreshCheck(reason), reason)
	}
}

func TestInboundRefreshHappened(t *testing.T) {
	t.Parallel()

	for reason, want := range map[string]mcpconnectionattempts.Code{
		"resource_mismatch":                     mcpconnectionattempts.CodeResourceMismatch,
		"agent_admission_misconfigured":         mcpconnectionattempts.CodeCredentialMisconfigured,
		"agent_credential_invalid":              mcpconnectionattempts.CodeConnectionChanged,
		"refresh_token_resource_mismatch":       mcpconnectionattempts.CodeResourceMismatch,
		"refresh_token_unknown_or_already_used": mcpconnectionattempts.CodeRefreshTokenUnknownOrAlreadyUsed,
		"refresh_token_client_mismatch":         mcpconnectionattempts.CodeRefreshTokenUsedByOtherApp,
		"refresh_token_expired":                 mcpconnectionattempts.CodeRefreshTokenExpired,
		"sign_in_revoked":                       mcpconnectionattempts.CodeSignInRevoked,
		"refresh_token_revoked":                 mcpconnectionattempts.CodeRefreshTokenRevoked,
		"refresh_token_replay_session_mismatch": mcpconnectionattempts.CodeStoredSessionInvalid,
		"refresh_token_replay_rejected":         mcpconnectionattempts.CodeRefreshTokenRevoked,
		"tool_selection_malformed":              mcpconnectionattempts.CodeStoredSessionInvalid,
		"tool_selection_resource_mismatch":      mcpconnectionattempts.CodeRefreshTokenRevoked,
		"agent_admission_denied":                mcpconnectionattempts.CodeNoServerAccess,
		"agent_not_admitted":                    mcpconnectionattempts.CodeSubjectNotAdmitted,
		"upstream_reconnect_required":           mcpconnectionattempts.CodeUpstreamReconnectRequired,
		"upstream_ema_refused":                  mcpconnectionattempts.CodeUpstreamEMARefused,
	} {
		code, ok := inboundRefreshHappened(reason)
		require.True(t, ok, reason)
		require.Equal(t, want, code, reason)
	}
	for _, reason := range []string{"invalid_request"} {
		_, ok := inboundRefreshHappened(reason)
		require.False(t, ok, reason)
	}
	code, ok := inboundRefreshHappened("a_reason_added_later")
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeInternalError, code, "an unknown reason is ours")
}

func TestAgentRefreshAdmissionReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err    error
		reason string
	}{
		{oops.C(oops.CodeUnauthorized), "agent_admission_misconfigured"},
		{agentCredentialRejected(), "agent_credential_invalid"},
		{oops.E(oops.CodeUnauthorized, runtimepolicy.ErrPrincipalNotAdmitted, "%s", oops.CodeUnauthorized.UserMessage()), "agent_not_admitted"},
		{oops.C(oops.CodeForbidden), "agent_admission_denied"},
		{errors.New("dependency unavailable"), "agent_admission_unavailable"},
		{oops.C(oops.CodeUnexpected), "agent_admission_unavailable"},
	} {
		require.Equal(t, tc.reason, agentAdmissionRejection(tc.err))
	}
}

func TestReplayLookupFailureReason(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"refresh_token_revoked", "sign_in_revoked"} {
		require.Equal(t, missing, replayLookupFailureReason(fmt.Errorf("lookup: %w", pgx.ErrNoRows), missing))
		require.Equal(t, "refresh_token_replay_lookup_failed", replayLookupFailureReason(errors.New("database unavailable"), missing))
	}
}

func agentRefreshFailureFixture(t *testing.T) (*ResolvedMcpEndpoint, agentSessionCredential) {
	t.Helper()
	endpoint := new(ResolvedMcpEndpoint)
	endpoint.OrganizationID = "org-test"
	endpoint.ProjectID = uuid.New()
	endpoint.UserSessionIssuerID = uuid.New()
	endpoint.McpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	target, ok := agentAuthorizationTarget(endpoint)
	require.True(t, ok)
	credential, err := newAgentSessionCredential(*target, "person")
	require.NoError(t, err)
	return endpoint, credential
}

func requireAgentRefreshFailureReason(t *testing.T, endpoint *ResolvedMcpEndpoint, credential agentSessionCredential, reason string) {
	t.Helper()
	subject := urn.NewAgentSubject(uuid.New())
	_, err := loadAgentSessionCredential(endpoint, subject, subject, pgtype.Text{String: endpoint.OrganizationID, Valid: true}, pgtype.Text{String: credential.AuthorizerUserID, Valid: true}, credential.DelegatedGrants, pgtype.Int4{Int32: credential.DelegatedGrantsVersion, Valid: true})
	require.Error(t, err)
	require.Equal(t, reason, agentAdmissionRejection(err))
	var public *oops.ShareableError
	require.ErrorAs(t, err, &public)
	require.Equal(t, oops.CodeUnauthorized, public.Code, "preserve the authorization response")
}

func TestAgentRefreshMalformedCredential(t *testing.T) {
	t.Parallel()
	endpoint, credential := agentRefreshFailureFixture(t)
	credential.DelegatedGrants = []byte("malformed")
	requireAgentRefreshFailureReason(t, endpoint, credential, "agent_credential_invalid")
}

func TestAgentRefreshBrokenEndpoint(t *testing.T) {
	t.Parallel()
	endpoint, credential := agentRefreshFailureFixture(t)
	endpoint.McpServerID.Valid = false
	requireAgentRefreshFailureReason(t, endpoint, credential, "agent_admission_misconfigured")
}
