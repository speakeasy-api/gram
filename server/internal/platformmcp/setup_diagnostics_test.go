package platformmcp

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

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
		evidence string
		state    ReadinessState
		want     SetupCategory
	}{
		{evidence: "invalid_url", state: ReadinessUnsupported, want: SetupCategoryInvalidURL},
		{evidence: "redirect_rejected", state: ReadinessUnsupported, want: SetupCategoryUnsafeTargetOrRedirect},
		{evidence: "probe_unreachable", state: ReadinessUnreachable, want: SetupCategoryUnreachable},
		{evidence: "probe_timeout", state: ReadinessUnreachable, want: SetupCategoryTimeout},
		{evidence: "invalid_mcp_response", state: ReadinessUnsupported, want: SetupCategoryInvalidMCPResponse},
		{evidence: "response_too_large", state: ReadinessUnsupported, want: SetupCategoryInvalidMCPResponse},
		{evidence: "upstream_authorization_required", state: ReadinessNeedsGramAuthorization, want: SetupCategoryAuthenticationRequired},
		{evidence: "required_header_missing", state: ReadinessNeedsConfiguration, want: SetupCategoryConfigurationRequired},
		{evidence: "oauth_metadata_incomplete", state: ReadinessNeedsProviderSetup, want: SetupCategoryOAuthMetadataIncomplete},
		{evidence: "dynamic_registration_unsupported", state: ReadinessNeedsProviderSetup, want: SetupCategoryDynamicRegistrationUnsupported},
		{evidence: "provider_authorization_rejected", state: ReadinessUnauthorized, want: SetupCategoryProviderAuthorizationRejected},
		{evidence: "readiness_unavailable", state: ReadinessDegraded, want: SetupCategoryTemporarilyUnavailable},
		{evidence: "tools_list_ok", state: ReadinessReady},
		{evidence: "provider_authorization_rejected", state: ReadinessReady},
	}
	for _, test := range tests {
		t.Run(test.evidence, func(t *testing.T) {
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
