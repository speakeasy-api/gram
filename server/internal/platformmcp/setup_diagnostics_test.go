package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

func TestSetupCategoryFromInspection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inspection DirectRemoteInspection
		want       SetupCategory
	}{
		{name: "anonymous", inspection: DirectRemoteInspection{Authentication: "anonymous"}},
		{name: "dynamic registration", inspection: DirectRemoteInspection{Authentication: "authentication_required", OAuthDiscovery: "available_dcr"}, want: SetupCategoryAuthenticationRequired},
		{name: "registration unsupported", inspection: DirectRemoteInspection{Authentication: "authentication_required", OAuthDiscovery: "available"}, want: SetupCategoryDynamicRegistrationUnsupported},
		{name: "metadata incomplete", inspection: DirectRemoteInspection{Authentication: "authentication_required", OAuthDiscovery: "incomplete"}, want: SetupCategoryOAuthMetadataIncomplete},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, setupCategoryFromInspection(test.inspection))
		})
	}
}

func TestSetupCategoryFromReadinessEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		evidence string
		state    ReadinessState
		want     SetupCategory
	}{
		{name: "invalid URL", evidence: "invalid_url", state: ReadinessUnsupported, want: SetupCategoryInvalidURL},
		{name: "redirect rejected", evidence: "redirect_rejected", state: ReadinessUnsupported, want: SetupCategoryUnsafeTargetOrRedirect},
		{name: "unreachable", evidence: "probe_unreachable", state: ReadinessUnreachable, want: SetupCategoryUnreachable},
		{name: "timeout", evidence: "probe_timeout", state: ReadinessUnreachable, want: SetupCategoryTimeout},
		{name: "invalid MCP response", evidence: "invalid_mcp_response", state: ReadinessUnsupported, want: SetupCategoryInvalidMCPResponse},
		{name: "oversized initialize response", evidence: "initialize_response_too_large", state: ReadinessUnsupported, want: SetupCategoryInvalidMCPResponse},
		{name: "oversized tools list response", evidence: "tools_list_response_too_large", state: ReadinessDegraded, want: SetupCategoryTemporarilyUnavailable},
		{name: "transient HTTP response", evidence: "probe_temporarily_unavailable", state: ReadinessDegraded, want: SetupCategoryTemporarilyUnavailable},
		{name: "authentication required", evidence: "upstream_authorization_required", state: ReadinessNeedsGramAuthorization, want: SetupCategoryAuthenticationRequired},
		{name: "configuration required", evidence: "required_header_missing", state: ReadinessNeedsConfiguration, want: SetupCategoryConfigurationRequired},
		{name: "OAuth metadata incomplete", evidence: "oauth_metadata_incomplete", state: ReadinessNeedsProviderSetup, want: SetupCategoryOAuthMetadataIncomplete},
		{name: "dynamic registration unsupported", evidence: "dynamic_registration_unsupported", state: ReadinessNeedsProviderSetup, want: SetupCategoryDynamicRegistrationUnsupported},
		{name: "provider authorization rejected", evidence: "provider_authorization_rejected", state: ReadinessUnauthorized, want: SetupCategoryProviderAuthorizationRejected},
		{name: "readiness unavailable", evidence: "readiness_unavailable", state: ReadinessDegraded, want: SetupCategoryTemporarilyUnavailable},
		{name: "unmanaged readiness", evidence: "readiness_not_managed", state: ReadinessUnsupported},
		{name: "ready", evidence: "tools_list_ok", state: ReadinessReady},
		{name: "ready suppresses stale evidence", evidence: "provider_authorization_rejected", state: ReadinessReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, setupCategoryFromReadiness(Readiness{State: test.state, EvidenceCode: test.evidence}))
		})
	}
}

func TestClassifyReadinessProbeFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantState    ReadinessState
		wantEvidence string
	}{
		{name: "deadline", err: context.DeadlineExceeded, wantState: ReadinessUnreachable, wantEvidence: "probe_timeout"},
		{name: "network timeout", err: &net.DNSError{IsTimeout: true}, wantState: ReadinessUnreachable, wantEvidence: "probe_timeout"},
		{name: "network unreachable", err: &net.DNSError{IsTemporary: true}, wantState: ReadinessUnreachable, wantEvidence: "probe_unreachable"},
		{name: "malformed JSON", err: &json.SyntaxError{}, wantState: ReadinessUnsupported, wantEvidence: "invalid_mcp_response"},
		{name: "invalid JSON shape", err: &json.UnmarshalTypeError{}, wantState: ReadinessUnsupported, wantEvidence: "invalid_mcp_response"},
		{name: "invalid JSON-RPC request", err: &jsonrpc.Error{Code: jsonrpc.CodeInvalidRequest}, wantState: ReadinessUnsupported, wantEvidence: "invalid_mcp_response"},
		{name: "valid JSON-RPC application error", err: &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound}, wantState: ReadinessUnreachable, wantEvidence: "probe_failed"},
		{name: "unclassified probe failure", err: errors.New("private upstream response detail"), wantState: ReadinessUnreachable, wantEvidence: "probe_failed"},
		{name: "nil failure", err: nil, wantState: ReadinessUnreachable, wantEvidence: "probe_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state, evidence := ClassifyReadinessProbeFailure(test.err)
			require.Equal(t, test.wantState, state)
			require.Equal(t, test.wantEvidence, evidence)
		})
	}
}

func TestSetupFailurePreservesBroadErrorIdentity(t *testing.T) {
	t.Parallel()

	err := setupFailure(SetupCategoryUnsafeTargetOrRedirect, ErrDirectRemoteRejected)
	require.ErrorIs(t, err, ErrDirectRemoteRejected)
	require.Equal(t, SetupCategoryUnsafeTargetOrRedirect, setupCategoryFromError(err))
}

func TestSanitizedSetupFailureDropsWrappedTargetDetails(t *testing.T) {
	t.Parallel()

	wrapped := &url.Error{
		Op:  "Get",
		URL: "https://private.example.test/mcp?tenant=sensitive",
		Err: setupFailure(SetupCategoryUnsafeTargetOrRedirect, ErrDirectRemoteRejected),
	}
	sanitized := sanitizedSetupFailure(wrapped)

	require.ErrorIs(t, sanitized, ErrDirectRemoteRejected)
	require.Equal(t, SetupCategoryUnsafeTargetOrRedirect, setupCategoryFromError(sanitized))
	require.NotContains(t, sanitized.Error(), "private.example.test")
	require.NotContains(t, sanitized.Error(), "sensitive")
}

func TestZeroValueSetupDiagnosticErrorDoesNotPanic(t *testing.T) {
	t.Parallel()

	err := &setupDiagnosticError{}
	require.Equal(t, "platform mcp setup diagnostic unavailable", err.Error())
	require.NoError(t, err.Unwrap())
}

func TestSetupRepairActionsUseCategorySpecificGuidance(t *testing.T) {
	t.Parallel()

	require.Equal(t, []RepairAction{{Kind: "review_remote_url", Label: "Review this MCP server's HTTPS URL in the dashboard"}}, setupRepairActions(SetupCategoryUnsafeTargetOrRedirect, ReadinessUnsupported))
	require.Empty(t, setupRepairActions("", ReadinessUnsupported))
}

func TestSetupRepairActionsAreBoundedAndContainNoSensitiveDetail(t *testing.T) {
	t.Parallel()

	for _, category := range []SetupCategory{
		SetupCategoryInvalidURL,
		SetupCategoryUnsafeTargetOrRedirect,
		SetupCategoryUnreachable,
		SetupCategoryTimeout,
		SetupCategoryInvalidMCPResponse,
		SetupCategoryAuthenticationRequired,
		SetupCategoryConfigurationRequired,
		SetupCategoryOAuthMetadataIncomplete,
		SetupCategoryDynamicRegistrationUnsupported,
		SetupCategoryProviderAuthorizationRejected,
		SetupCategoryTemporarilyUnavailable,
	} {
		actions := setupRepairActions(category, ReadinessDegraded)
		require.NotEmpty(t, actions, category)
		require.LessOrEqual(t, len(actions), 3, category)
		for _, action := range actions {
			require.NotContains(t, action.Label, "private upstream response detail")
			require.NotContains(t, action.Label, "token")
			require.NotContains(t, action.Label, "header")
		}
	}
}
