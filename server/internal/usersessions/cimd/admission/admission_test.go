package admission

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Shared fixtures for the package's tests. claudeCodeURL is a catalog
// member; unknownURL never is; customURL stands for a URL an issuer would
// carry as its own entry.
const (
	claudeCodeURL = "https://claude.ai/oauth/claude-code-client-metadata"
	unknownURL    = "https://evil.example.com/oauth/client-metadata.json"
	customURL     = "https://internal.example.com/oauth/client-metadata.json"

	// chatGPTConnectorURL is admitted by a wildcard catalog entry rather
	// than an exact one, which is what separates AdmitCatalogPattern from
	// AdmitCatalogExact.
	chatGPTConnectorURL = "https://chatgpt.com/oauth/connector-abc123/client.json"
)

// TestEvaluate_ModeMatrix is the acceptance matrix: every mode against a
// preset URL, a URL the issuer would carry as a custom entry, and an
// unknown URL. OutcomeCheckCustom is the only outcome that costs a query.
func TestEvaluate_ModeMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		mode     Mode
		clientID string
		want     Decision
	}{
		{name: "disabled/preset", mode: ModeDisabled, clientID: claudeCodeURL, want: Decision{Outcome: OutcomeDeny, Admit: "", Denial: DenialDisabled}},
		{name: "disabled/custom", mode: ModeDisabled, clientID: customURL, want: Decision{Outcome: OutcomeDeny, Admit: "", Denial: DenialDisabled}},
		{name: "disabled/unknown", mode: ModeDisabled, clientID: unknownURL, want: Decision{Outcome: OutcomeDeny, Admit: "", Denial: DenialDisabled}},

		{name: "presets/preset", mode: ModePresets, clientID: claudeCodeURL, want: Decision{Outcome: OutcomeAdmit, Admit: AdmitCatalogExact, Denial: ""}},
		{name: "presets/pattern", mode: ModePresets, clientID: chatGPTConnectorURL, want: Decision{Outcome: OutcomeAdmit, Admit: AdmitCatalogPattern, Denial: ""}},
		{name: "presets/custom", mode: ModePresets, clientID: customURL, want: Decision{Outcome: OutcomeCheckCustom, Admit: "", Denial: ""}},
		{name: "presets/unknown", mode: ModePresets, clientID: unknownURL, want: Decision{Outcome: OutcomeCheckCustom, Admit: "", Denial: ""}},

		{name: "reporting/preset", mode: ModeReporting, clientID: claudeCodeURL, want: Decision{Outcome: OutcomeAdmit, Admit: AdmitCatalogExact, Denial: ""}},
		{name: "reporting/pattern", mode: ModeReporting, clientID: chatGPTConnectorURL, want: Decision{Outcome: OutcomeAdmit, Admit: AdmitCatalogPattern, Denial: ""}},
		{name: "reporting/custom", mode: ModeReporting, clientID: customURL, want: Decision{Outcome: OutcomeCheckCustom, Admit: "", Denial: ""}},
		{name: "reporting/unknown", mode: ModeReporting, clientID: unknownURL, want: Decision{Outcome: OutcomeCheckCustom, Admit: "", Denial: ""}},

		{name: "open/preset", mode: ModeOpen, clientID: claudeCodeURL, want: Decision{Outcome: OutcomeAdmit, Admit: AdmitOpen, Denial: ""}},
		{name: "open/custom", mode: ModeOpen, clientID: customURL, want: Decision{Outcome: OutcomeAdmit, Admit: AdmitOpen, Denial: ""}},
		{name: "open/unknown", mode: ModeOpen, clientID: unknownURL, want: Decision{Outcome: OutcomeAdmit, Admit: AdmitOpen, Denial: ""}},
	}

	for _, tc := range cases {
		require.Equalf(t, tc.want, Evaluate(tc.mode, tc.clientID), "case %s", tc.name)
	}
}

// TestEvaluate_DecisionCarriesExactlyOneReason pins the invariant the
// Decision struct exists to express: Admit and Denial are mutually
// exclusive, and OutcomeCheckCustom carries neither because the decision is
// not final yet.
func TestEvaluate_DecisionCarriesExactlyOneReason(t *testing.T) {
	t.Parallel()

	inputs := []string{claudeCodeURL, chatGPTConnectorURL, customURL, unknownURL, strings.Repeat("x", MaxClientIDLength+1)}
	for _, mode := range append(Modes(), Mode("nonsense")) {
		for _, clientID := range inputs {
			decision := Evaluate(mode, clientID)
			switch decision.Outcome {
			case OutcomeAdmit:
				require.NotEmpty(t, decision.Admit, "admit decision must carry a reason")
				require.Empty(t, decision.Denial, "admit decision must not carry a denial reason")
			case OutcomeDeny:
				require.NotEmpty(t, decision.Denial, "denial must carry a reason")
				require.Empty(t, decision.Admit, "denial must not carry an admit reason")
			case OutcomeCheckCustom:
				require.Empty(t, decision.Admit, "deferred decision must carry no reason")
				require.Empty(t, decision.Denial, "deferred decision must carry no reason")
			default:
				t.Fatalf("unexpected outcome %q", decision.Outcome)
			}
		}
	}
}

// TestEvaluate_NeverReturnsAdmitCustom: AdmitCustom depends on a database
// lookup, so it is the caller's to supply and must never originate here.
func TestEvaluate_NeverReturnsAdmitCustom(t *testing.T) {
	t.Parallel()

	for _, mode := range Modes() {
		for _, clientID := range []string{claudeCodeURL, chatGPTConnectorURL, customURL, unknownURL} {
			require.NotEqual(t, AdmitCustom, Evaluate(mode, clientID).Admit)
		}
	}
}

// TestEvaluate_ReportingDecidesExactlyAsPresets is the property the rollout
// rests on. Reporting exists to predict what presets will do, so any
// divergence would make the measurement worthless — an operator would see a
// clean reporting signal and then break clients on the switch.
//
// The unset default resolves to reporting, so this also covers "whatever
// presets does, an unconfigured issuer evaluates identically".
func TestEvaluate_ReportingDecidesExactlyAsPresets(t *testing.T) {
	t.Parallel()

	unsetMode, _ := ResolveMode("", false)
	require.Equal(t, ModeReporting, unsetMode)

	inputs := []string{
		claudeCodeURL,
		chatGPTConnectorURL,
		customURL,
		unknownURL,
		strings.Repeat("x", MaxClientIDLength+1),
		"",
	}
	for _, clientID := range inputs {
		require.Equalf(t, Evaluate(ModePresets, clientID), Evaluate(ModeReporting, clientID),
			"reporting and presets must agree on %q", clientID)
	}
}

// TestEvaluate_OversizedDeniedBeforeCustomLookup: an oversized client_id in
// presets mode must be denied outright, never handed to the database as a
// query parameter. OutcomeCheckCustom here would be the bug.
func TestEvaluate_OversizedDeniedBeforeCustomLookup(t *testing.T) {
	t.Parallel()

	oversized := "https://client.example.com/" + strings.Repeat("a", MaxClientIDLength)
	require.Greater(t, len(oversized), MaxClientIDLength)

	decision := Evaluate(ModePresets, oversized)
	require.Equal(t, OutcomeDeny, decision.Outcome)
	require.Equal(t, DenialOversized, decision.Denial)
}

// TestEvaluate_OversizedAdmittedWhenOpen: open mode skips admission
// entirely, so the length cap is left to the resolver's own validation,
// which reports it as a spec violation with its established telemetry.
func TestEvaluate_OversizedAdmittedWhenOpen(t *testing.T) {
	t.Parallel()

	oversized := "https://client.example.com/" + strings.Repeat("a", MaxClientIDLength)

	require.Equal(t, OutcomeAdmit, Evaluate(ModeOpen, oversized).Outcome)
}

// TestEvaluate_UnknownModeFailsClosed guards the backstop branch for a Mode
// built without going through ResolveMode.
func TestEvaluate_UnknownModeFailsClosed(t *testing.T) {
	t.Parallel()

	decision := Evaluate(Mode("nonsense"), claudeCodeURL)
	require.Equal(t, OutcomeDeny, decision.Outcome)
	require.Equal(t, DenialUnknownMode, decision.Denial)
}

// TestEvaluate_ZeroValueModeFailsClosed: the Mode zero value must never be
// an accident that admits traffic.
func TestEvaluate_ZeroValueModeFailsClosed(t *testing.T) {
	t.Parallel()

	var zero Mode
	decision := Evaluate(zero, claudeCodeURL)
	require.Equal(t, OutcomeDeny, decision.Outcome)
	require.Equal(t, DenialUnknownMode, decision.Denial)
}
