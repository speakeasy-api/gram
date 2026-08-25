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
			// The rejections classified here are the provider's answer to
			// Gram's own calls, so a probe that authorized with the same
			// credentials contradicts them. Naming the caller at fault would
			// blame the one party this evidence says nothing about.
			name:           "fresh ready with unauthorized calls is indeterminate",
			readiness:      freshReadiness(ReadinessReady),
			readinessFound: true,
			server:         outcomeTotals{Total: 20, Success: 2, Unauthorized: 18},
			wantFault:      FaultIndeterminate,
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
			// The organization totals contain this server's own calls. When it
			// is the only thing generating traffic, comparing against them
			// unmodified makes every pattern look organization-wide and
			// downgrades the diagnosis to indeterminate — silencing the answer
			// in exactly the case a small tenant asks for it.
			name:         "sole server in the organization still gets its own diagnosis",
			server:       outcomeTotals{Total: 20, Success: 2, Unauthorized: 18},
			organization: outcomeTotals{Total: 20, Success: 2, Unauthorized: 18},
			wantFault:    FaultGramConfiguration,
			wantReason:   reasonUnauthorizedDominant,
			wantScope:    FaultScopeUnknown,
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

// TestCompareScope_ComparesAgainstTheRestOfTheOrganization pins that the floor
// and the rates are both taken over the organization minus this server. The
// organization totals a caller of attributeFault supplies always contain the
// server under diagnosis, so every case here includes it.
func TestCompareScope_ComparesAgainstTheRestOfTheOrganization(t *testing.T) {
	t.Parallel()

	server := outcomeTotals{Total: 20, Success: 2, Unauthorized: 18}
	withServer := func(rest outcomeTotals) outcomeTotals {
		return outcomeTotals{
			Total:        server.Total + rest.Total,
			Success:      server.Success + rest.Success,
			Unauthorized: server.Unauthorized + rest.Unauthorized,
			ClientError:  rest.ClientError,
			ServerError:  rest.ServerError,
			Failed:       rest.Failed,
			Unknown:      rest.Unknown,
		}
	}

	// Below the floor the comparison is noise, so the scope is reported unknown
	// rather than asserted from a handful of calls.
	require.Equal(t, FaultScopeUnknown, compareScope(server, withServer(outcomeTotals{Total: 19, Success: 19})))
	require.Equal(t, FaultScopeServerSpecific, compareScope(server, withServer(outcomeTotals{Total: 20, Success: 20})))

	// The rest of the organization fails the same way, so nothing here is
	// specific to this server.
	require.Equal(t, FaultScopeOrganizationWide, compareScope(server, withServer(outcomeTotals{Total: 100, Success: 10, Unauthorized: 90})))

	// A server that is the organization's only traffic leaves nothing to
	// compare against, which is unknown rather than organization-wide.
	require.Equal(t, FaultScopeUnknown, compareScope(server, server))

	require.Equal(t, FaultScopeUnknown, compareScope(outcomeTotals{}, outcomeTotals{Total: 500, Success: 500}))
}
