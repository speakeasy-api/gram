package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func freshReadiness(state ReadinessState) Readiness {
	return Readiness{State: state, Fresh: true}
}

func TestAttributeFault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		readiness      Readiness
		readinessFound bool
		server         outcomeTotals
		organization   outcomeTotals
		wantFault      Fault
		wantReason     string
		wantScope      FaultScope
	}{
		{
			// Silence is not health. A server nobody called cannot be
			// pronounced working.
			name:       "no observations is indeterminate",
			server:     outcomeTotals{},
			wantFault:  FaultIndeterminate,
			wantReason: reasonNoObservations,
			wantScope:  FaultScopeUnknown,
		},
		{
			name:       "no failures is no fault",
			server:     outcomeTotals{Total: 40, Success: 40},
			wantFault:  FaultNone,
			wantReason: reasonNoFailures,
			wantScope:  FaultScopeUnknown,
		},
		{
			// Calls nobody could classify are not successes. Counting the
			// absence of classified failures as health is the same mistake as
			// reading an empty result as a healthy one.
			name:       "unclassified-only observations are indeterminate",
			server:     outcomeTotals{Total: 12, Unknown: 12},
			wantFault:  FaultIndeterminate,
			wantReason: reasonUnclassifiedOnly,
			wantScope:  FaultScopeUnknown,
		},
		{
			name:       "successes alongside unclassified calls are still no fault",
			server:     outcomeTotals{Total: 12, Success: 8, Unknown: 4},
			wantFault:  FaultNone,
			wantReason: reasonNoFailures,
			wantScope:  FaultScopeUnknown,
		},
		{
			// A fresh readiness result is a direct statement about this
			// server's Gram-side setup and outranks inference from outcomes.
			name:           "not-ready readiness attributes to gram configuration",
			readiness:      freshReadiness(ReadinessNeedsGramAuthorization),
			readinessFound: true,
			server:         outcomeTotals{Total: 10, Success: 2, ServerError: 8},
			wantFault:      FaultGramConfiguration,
			wantReason:     reasonReadinessNotReady,
			wantScope:      FaultScopeUnknown,
		},
		{
			name:       "unauthorized dominant without readiness is gram configuration",
			server:     outcomeTotals{Total: 20, Success: 2, Unauthorized: 18},
			wantFault:  FaultGramConfiguration,
			wantReason: reasonUnauthorizedDominant,
			wantScope:  FaultScopeUnknown,
		},
		{
			// The probe authorized with the credentials the server holds, so
			// the rejections are about how callers present themselves.
			name:           "fresh ready exonerates gram on unauthorized calls",
			readiness:      freshReadiness(ReadinessReady),
			readinessFound: true,
			server:         outcomeTotals{Total: 20, Success: 2, Unauthorized: 18},
			wantFault:      FaultClient,
			wantReason:     reasonReadyAndFailing,
			wantScope:      FaultScopeUnknown,
		},
		{
			name:       "server errors without readiness point upstream",
			server:     outcomeTotals{Total: 20, Success: 2, ServerError: 18},
			wantFault:  FaultProvider,
			wantReason: reasonServerErrorDominant,
			wantScope:  FaultScopeUnknown,
		},
		{
			// The provider answered a probe yet fails real calls. That is a
			// real contradiction, and saying so beats picking a side.
			name:           "fresh ready with server errors is indeterminate",
			readiness:      freshReadiness(ReadinessReady),
			readinessFound: true,
			server:         outcomeTotals{Total: 20, Success: 2, ServerError: 18},
			wantFault:      FaultIndeterminate,
			wantReason:     reasonReadyAndFailing,
			wantScope:      FaultScopeUnknown,
		},
		{
			name:       "client errors are the caller's",
			server:     outcomeTotals{Total: 20, Success: 2, ClientError: 18},
			wantFault:  FaultClient,
			wantReason: reasonClientErrorDominant,
			wantScope:  FaultScopeUnknown,
		},
		{
			// An even spread across failure classes names no candidate; the
			// leading class by a single call is not evidence.
			name:       "evenly mixed failures stay indeterminate",
			server:     outcomeTotals{Total: 30, Success: 0, Unauthorized: 10, ClientError: 10, ServerError: 10},
			wantFault:  FaultIndeterminate,
			wantReason: reasonMixedFailures,
			wantScope:  FaultScopeUnknown,
		},
		{
			// Everything in the organization fails the same way, so this
			// server's own configuration is not what distinguishes it.
			name:         "organization-wide unauthorized pattern is not this server's configuration",
			server:       outcomeTotals{Total: 20, Success: 2, Unauthorized: 18},
			organization: outcomeTotals{Total: 200, Success: 20, Unauthorized: 180},
			wantFault:    FaultIndeterminate,
			wantReason:   reasonUnauthorizedDominant,
			wantScope:    FaultScopeOrganizationWide,
		},
		{
			name:         "server-specific unauthorized pattern is this server's configuration",
			server:       outcomeTotals{Total: 20, Success: 2, Unauthorized: 18},
			organization: outcomeTotals{Total: 200, Success: 190, Unauthorized: 10},
			wantFault:    FaultGramConfiguration,
			wantReason:   reasonUnauthorizedDominant,
			wantScope:    FaultScopeServerSpecific,
		},
		{
			// A stale readiness result states nothing about the system as it is
			// now, so it must not exonerate anything.
			name:           "stale ready does not exonerate",
			readiness:      Readiness{State: ReadinessReady, Fresh: false},
			readinessFound: true,
			server:         outcomeTotals{Total: 20, Success: 2, Unauthorized: 18},
			wantFault:      FaultGramConfiguration,
			wantReason:     reasonUnauthorizedDominant,
			wantScope:      FaultScopeUnknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			attribution := attributeFault(test.readiness, test.readinessFound, test.server, test.organization)
			require.Equal(t, test.wantFault, attribution.Fault)
			require.Equal(t, test.wantReason, attribution.Reason)
			require.Equal(t, test.wantScope, attribution.Scope)
		})
	}
}

// TestAttributeFault_ExonerationRequiresFreshReady pins the one condition that
// lets a diagnosis clear Gram's configuration and the provider at once.
func TestAttributeFault_ExonerationRequiresFreshReady(t *testing.T) {
	t.Parallel()

	server := outcomeTotals{Total: 10, Success: 10}

	require.True(t, attributeFault(freshReadiness(ReadinessReady), true, server, outcomeTotals{}).ReadinessExonerates)
	require.False(t, attributeFault(freshReadiness(ReadinessReady), false, server, outcomeTotals{}).ReadinessExonerates)
	require.False(t, attributeFault(Readiness{State: ReadinessReady, Fresh: false}, true, server, outcomeTotals{}).ReadinessExonerates)
	require.False(t, attributeFault(freshReadiness(ReadinessDegraded), true, server, outcomeTotals{}).ReadinessExonerates)
}

func TestCompareScope_NeedsEnoughOrganizationTraffic(t *testing.T) {
	t.Parallel()

	server := outcomeTotals{Total: 20, Success: 2, Unauthorized: 18}

	// Below the floor the comparison is noise, so the scope is reported unknown
	// rather than asserted from a handful of calls.
	require.Equal(t, FaultScopeUnknown, compareScope(server, outcomeTotals{Total: 19, Success: 19}))
	require.Equal(t, FaultScopeServerSpecific, compareScope(server, outcomeTotals{Total: 20, Success: 20}))
	require.Equal(t, FaultScopeUnknown, compareScope(outcomeTotals{}, outcomeTotals{Total: 500, Success: 500}))
}
