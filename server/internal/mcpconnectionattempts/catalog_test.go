package mcpconnectionattempts

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// frozenCodes is append-only: stored rows and dashboards use these names. A
// failure here means a code was renamed or removed. Put it back. To add a code,
// declare it in the catalog and add it to the end of this list.
var frozenCodes = []string{
	"cimd_admission_denied",
	"ai_tool_blocked",
	"not_org_member",
	"no_server_access",
	"network_denied",
	"custom_domain_required",
	"custom_domain_not_ready",
	"idp_unavailable",
	"ai_tool_check_unavailable",
	"cimd_fetch_failed",
	"cimd_metadata_invalid",
	"registration_invalid",
	"unknown_client_id",
	"redirect_uri_mismatch",
	"resource_mismatch",
	"pkce_mismatch",
	"code_not_found_or_expired",
	"server_not_found",
	"expired",
	"session_changed",
	"canceled",
	"endpoint_not_agent_addressable",
	"subject_not_admitted",
	"client_not_cimd",
	"subject_not_provisioned",
	"invalid_request",
	"resource_not_available",
	"assertion_invalid",
	"assertion_other",
	"upstream_credential_choice",
	"upstream_sign_in_not_set_up",
	"issuer_not_trusted",
	"assertion_verifier_misconfigured",
	"assertion_check_unavailable",
	"provider_oauth_invalid_client",
	"provider_oauth_invalid_scope",
	"provider_oauth_invalid_grant",
	"provider_denied",
	"issuer_mismatch",
	"connection_changed",
	"unrenewable_token",
	"rejected_by_member",
	"provider_rate_limited",
	"provider_unavailable",
	"provider_server_error",
	"provider_unreachable",
	"login_expired",
	"shared_credential_rejected",
	"static_key_rejected",
	"configuration_required",
	"access_denied",
	"scope_policy_denied",
	"invalid_target",
	"malformed_assertion",
	"insufficient_scope",
	"malformed_response",
	"transient_failure",
	"idp_transient_failure",
	"provider_transient_failure",
	"reauthentication_required",
	"ineligible_identity",
	"tool_permission_denied",
	"mcp_tool_calls_paused",
	"policy_blocked",
	"usage_limit_reached",
	"tool_not_approved",
	"invalid_bearer_token",
	"auth_check_unavailable",
	"invalid_remote_session",
	"credential_misconfigured",
	"remote_session_unavailable",
	"provider_rejected",
	"ema_provider_rejected",
	"tunnel_unavailable",
	"killswitch_check_unavailable",
	"member_tools_missing",
	"refreshed_unverified",
	"refresh_token_expired",
	"sign_in_revoked",
	"refresh_token_revoked",
	"refresh_token_unknown_or_already_used",
	"upstream_reconnect_required",
	"upstream_ema_refused",
	"invalid_grant",
	"refresh_invalid_client",
	"no_grant",
	"refresh_rejected",
	"internal_error",
	"offline_not_requested",
	"offline_unsupported",
	"renewal_refused",
	"renewal_misconfigured",
	"invalid_identity",
	"renewal_retryable",
	"renewal_ambiguous",
	"needs_sign_in",
	"consent_not_approved",
	"client_authentication_failed",
	"sign_in_provider_error",
}

// frozenResults pins each result's stored name, append-only like the codes.
var frozenResults = map[string]Result{
	"accepted":                  ResultAccepted,
	"signed_in":                 ResultSignedIn,
	"code_issued":               ResultCodeIssued,
	"refreshed":                 ResultRefreshed,
	"refresh_replayed":          ResultRefreshReplayed,
	"adopted_concurrent_winner": ResultAdoptedConcurrentWinner,
	"renewed":                   ResultRenewed,
	"assertion_issued":          ResultAssertionIssued,
	"allowed":                   ResultAllowed,
	"listed":                    ResultListed,
	"complete":                  ResultComplete,
	"partial":                   resultPartial,
	"failed":                    resultFailed,
	"policy_blocked":            resultPolicyBlocked,
	"declined":                  resultDeclined,
	"needs_sign_in":             resultNeedsSignIn,
	"needs_consent":             resultNeedsConsent,
}

// frozenSteps pins each step's stored name, append-only like the codes.
var frozenSteps = map[string]Step{
	"inbound_authorize": StepInboundAuthorize,
	"inbound_sign_in":   StepInboundSignIn,
	"inbound_consent":   StepInboundConsent,
	"inbound_token":     StepInboundToken,
	"provider_sign_in":  StepProviderSignIn,
	"provider_token":    StepProviderToken,
	"provider_refresh":  StepProviderRefresh,
	"sign_in_renewal":   StepSignInRenewal,
	"upstream_ema":      StepUpstreamEMA,
	"mcp_request":       StepMCPRequest,
	"discovery_listing": StepDiscoveryListing,
	"discovery_member":  StepDiscoveryMember,
}

const (
	// maxMessageBytes keeps a message to one short sentence.
	maxMessageBytes = 90

	// jargon is what a non-technical reader should never see.
	jargon = "oauth|pkce|cimd|dcr|jwt|bearer|http|grant|redirect|upstream|scope|assertion|id-jag"
)

func TestCatalog_CodesAreAppendOnly(t *testing.T) {
	t.Parallel()

	got := slices.Collect(maps.Keys(catalog))
	for _, name := range frozenCodes {
		require.Contains(t, got, name, "code was renamed or removed")
	}
	require.ElementsMatch(t, frozenCodes, got, "add new codes to frozenCodes")
}

func TestCatalog_ResultsAndStepsAreNotRenamed(t *testing.T) {
	t.Parallel()

	for name, result := range frozenResults {
		require.Equal(t, name, result.String())
	}
	for name, step := range frozenSteps {
		require.Equal(t, name, step.String())
	}
	require.Contains(t, frozenResults, "needs_sign_in")
	require.Contains(t, frozenResults, "needs_consent")
	require.Contains(t, frozenSteps, "discovery_member", "the rollup's recovered column filters on this")
	require.Contains(t, frozenResults, "listed", "the rollup's recovered column filters on this")
}

func TestCatalog_RetiredNamesStayRetired(t *testing.T) {
	t.Parallel()

	for _, retired := range []string{"credential_blocked", "consent_blocked", "unknown", "session-changed", "ambiguous", "retryable", "configuration", "refused", "rejected"} {
		_, ok := MeaningOf(retired)
		require.False(t, ok, retired)
		require.NotContains(t, frozenResults, retired)
	}
}

func TestCatalog_EveryCodeHasAMeaningPeopleCanRead(t *testing.T) {
	t.Parallel()

	for name, meaning := range catalog {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, []Outcome{OutcomeConnected, OutcomeFailed, OutcomeBlocked, OutcomeDeclined}, meaning.Outcome)
			require.NotEmpty(t, meaning.WhatHappened)
			require.NotEmpty(t, meaning.WhatToDo)
			require.LessOrEqual(t, len(meaning.WhatHappened), maxMessageBytes)
			require.LessOrEqual(t, len(meaning.WhatToDo), maxMessageBytes)
			require.NotEmpty(t, meaning.WhoFixes.WhoToAsk(), "says who to ask")
			require.NotEmpty(t, meaning.result.String())
			for _, text := range []string{meaning.WhatHappened, meaning.WhatToDo} {
				for word := range strings.SplitSeq(jargon, "|") {
					require.NotContains(t, strings.ToLower(text), word)
				}
			}
		})
	}
}

// Stage decides the party in the build reference, so stages are separate codes.
func TestCatalog_StageDependentPartiesAreSeparateCodes(t *testing.T) {
	t.Parallel()

	cases := map[Code]WhoFixes{
		CodeTransientFailure:           WhoSpeakeasy,
		CodeIDPTransientFailure:        WhoProvider,
		CodeProviderTransientFailure:   WhoProvider,
		CodeProviderRejected:           WhoPerson,
		CodeEMAProviderRejected:        WhoAdmin,
		CodeUpstreamReconnectRequired:  WhoPerson,
		CodeUpstreamEMARefused:         WhoAdmin,
		CodeUpstreamCredentialChoice:   WhoApp,
		CodeUpstreamSignInNotSetUp:     WhoAdmin,
		CodeProviderOAuthInvalidClient: WhoAdmin,
		CodeRefreshInvalidClient:       WhoPerson,
	}
	for code, who := range cases {
		meaning, ok := MeaningOf(code.String())
		require.True(t, ok, code)
		require.Equal(t, who, meaning.WhoFixes, code)
	}
}

// The merged pairs each had one meaning and one party, so one code stands for both.
func TestCatalog_SameMeaningHasOneCode(t *testing.T) {
	t.Parallel()

	for _, merged := range []string{"issuer_untrusted", "client_not_admitted", "client_blocked", "subject_not_authorized", "invalid_client", "rate_limited", "unreachable", "client_check_unavailable", "client_metadata_unavailable", "client_invalid", "tunnel_unreachable", "upstream_error", "idp_misconfigured"} {
		_, ok := MeaningOf(merged)
		require.False(t, ok, merged)
	}
}

func TestDefine_RefusesADuplicateName(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		define(CodeCanceled.String(), OutcomeFailed, WhoAdmin, "a", "b")
	})
	meaning, ok := MeaningOf(CodeCanceled.String())
	require.True(t, ok)
	require.Equal(t, OutcomeDeclined, meaning.Outcome)
}
