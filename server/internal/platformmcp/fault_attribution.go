package platformmcp

// Fault is where a diagnosed problem lives. The set is closed and the values
// are the only vocabulary a caller receives.
type Fault string

const (
	// FaultNone is a server with no evidence of a problem in the window.
	FaultNone Fault = "none"
	// FaultGramConfiguration is a problem in how the MCP is configured in Gram:
	// missing or expired authorization, incomplete required configuration.
	FaultGramConfiguration Fault = "gram_configuration"
	// FaultProvider is a problem upstream of Gram — the provider the MCP
	// fronts is failing or unreachable.
	FaultProvider Fault = "provider"
	// FaultClient is a problem in the calling client: malformed or rejected
	// requests that never became a provider call.
	FaultClient Fault = "client"
	// FaultIndeterminate is the honest answer when the evidence does not
	// separate the candidates. It is always preferred over a guess.
	FaultIndeterminate Fault = "indeterminate"
)

// FaultAttribution is a diagnosis and the evidence it rests on. Reasons are
// server-authored codes from a closed set, never provider or client text.
type FaultAttribution struct {
	Fault Fault `json:"fault"`
	// Reason names the single rule that decided this attribution.
	Reason string `json:"reason"`
	// ReadinessExonerates records whether a fresh, ready server-side readiness
	// result was available. When true, Gram-side configuration and the provider
	// are both known good as of that check.
	ReadinessExonerates bool `json:"readiness_exonerates"`
	// Scope says whether the failure pattern is confined to this server or
	// present across the organization; an organization-wide pattern points away
	// from this server's own configuration.
	Scope FaultScope `json:"scope"`
}

// FaultScope is the server-computed answer to "is this only happening here?".
type FaultScope string

const (
	FaultScopeServerSpecific   FaultScope = "server_specific"
	FaultScopeOrganizationWide FaultScope = "organization_wide"
	// FaultScopeUnknown is returned when the organization-wide comparison had
	// too little traffic to say either way.
	FaultScopeUnknown FaultScope = "unknown"
)

const (
	reasonNoObservations       = "no_observations"
	reasonNoFailures           = "no_failures"
	reasonUnclassifiedOnly     = "unclassified_observations_only"
	reasonReadyAndFailing      = "ready_but_failing"
	reasonUnauthorizedDominant = "unauthorized_dominant"
	reasonServerErrorDominant  = "server_error_dominant"
	reasonClientErrorDominant  = "client_error_dominant"
	reasonMixedFailures        = "mixed_failures"
	reasonReadinessNotReady    = "readiness_not_ready"
)

// outcomeTotals is one server's calls in the window, split by outcome class.
type outcomeTotals struct {
	Total        int64
	Success      int64
	Unauthorized int64
	ClientError  int64
	ServerError  int64
	Failed       int64
	Unknown      int64
}

func (t outcomeTotals) failures() int64 {
	return t.Unauthorized + t.ClientError + t.ServerError + t.Failed
}

// without removes one server's calls from a wider tally, so a comparison can be
// taken against the rest of the organization rather than against a total that
// already contains the server under diagnosis.
//
// Each class is floored at zero. The two tallies come from separate reads, so a
// subset can legitimately arrive larger than the set it was taken from — a call
// observed by one read and not the other — and a negative class would otherwise
// travel into a rate.
func (t outcomeTotals) without(other outcomeTotals) outcomeTotals {
	sub := func(a, b int64) int64 {
		return max(a-b, 0)
	}
	return outcomeTotals{
		Total:        sub(t.Total, other.Total),
		Success:      sub(t.Success, other.Success),
		Unauthorized: sub(t.Unauthorized, other.Unauthorized),
		ClientError:  sub(t.ClientError, other.ClientError),
		ServerError:  sub(t.ServerError, other.ServerError),
		Failed:       sub(t.Failed, other.Failed),
		Unknown:      sub(t.Unknown, other.Unknown),
	}
}

// dominant reports whether one failure class accounts for most of the failures.
// A single class carrying the majority is a signal; an even spread is not, and
// resolves to indeterminate rather than to whichever class happened to lead.
func (t outcomeTotals) dominant() (string, bool) {
	failures := t.failures()
	if failures == 0 {
		return "", false
	}
	majority := failures/2 + 1
	switch {
	case t.Unauthorized >= majority:
		return reasonUnauthorizedDominant, true
	case t.ServerError >= majority:
		return reasonServerErrorDominant, true
	case t.ClientError >= majority:
		return reasonClientErrorDominant, true
	default:
		return "", false
	}
}

// organizationWideComparisonFloor is the least organization-wide traffic that
// makes the scope comparison meaningful. Below it the comparison is noise and
// the scope is reported unknown rather than asserted.
const organizationWideComparisonFloor = 20

// organizationWideFailureMargin is how much higher this server's failure rate
// must be than the organization's before the pattern counts as specific to it.
const organizationWideFailureMargin = 0.2

// attributeFault composes a diagnosis from the three independent pieces of
// evidence the RFC names: the latest server-side readiness result, the
// outcome breakdown for this server, and the same breakdown across the
// organization.
//
// It never guesses. Every branch that cannot separate the candidates returns
// FaultIndeterminate with the reason that left it undecided.
func attributeFault(readiness Readiness, readinessFound bool, server, organization outcomeTotals) FaultAttribution {
	scope := compareScope(server, organization)
	// A fresh ready result is a positive statement made by Gram's own probe:
	// the configuration resolved and the provider answered. It cannot exonerate
	// anything if it is stale or missing.
	exonerates := readinessFound && readiness.Fresh && readiness.State == ReadinessReady

	attribution := FaultAttribution{
		Fault:               FaultIndeterminate,
		Reason:              reasonMixedFailures,
		ReadinessExonerates: exonerates,
		Scope:               scope,
	}

	if server.Total == 0 {
		attribution.Fault = FaultIndeterminate
		attribution.Reason = reasonNoObservations
		return attribution
	}
	if server.failures() == 0 {
		// Calls whose trace carried neither a status nor a result are not
		// evidence of success. A window that produced only those says nothing
		// about the server, so it is indeterminate rather than healthy.
		if server.Success == 0 {
			attribution.Fault = FaultIndeterminate
			attribution.Reason = reasonUnclassifiedOnly
			return attribution
		}
		attribution.Fault = FaultNone
		attribution.Reason = reasonNoFailures
		return attribution
	}

	// Readiness states other than ready are a direct statement about this
	// server's Gram-side setup, and outrank inference from call outcomes.
	if readinessFound && readiness.Fresh && readiness.State != ReadinessReady {
		attribution.Fault = FaultGramConfiguration
		attribution.Reason = reasonReadinessNotReady
		return attribution
	}

	dominant, decided := server.dominant()
	if !decided {
		return attribution
	}
	attribution.Reason = dominant

	switch dominant {
	case reasonClientErrorDominant:
		// Requests rejected before they became a provider call. Neither Gram's
		// configuration nor the provider produced them.
		attribution.Fault = FaultClient
	case reasonUnauthorizedDominant:
		if exonerates {
			// A contradiction, not a diagnosis: the status classified here is
			// the one the provider returned to Gram's own call, so a probe that
			// authorized successfully and calls the provider rejects cannot
			// both describe the same credentials. Reporting the caller at fault
			// would blame the one party this evidence says nothing about.
			attribution.Fault = FaultIndeterminate
			attribution.Reason = reasonReadyAndFailing
			return attribution
		}
		attribution.Fault = FaultGramConfiguration
	case reasonServerErrorDominant:
		if exonerates {
			// The provider answered a probe but is failing real calls: still
			// upstream, but the readiness check cannot confirm it, so say so.
			attribution.Fault = FaultIndeterminate
			attribution.Reason = reasonReadyAndFailing
			return attribution
		}
		attribution.Fault = FaultProvider
	default:
		attribution.Fault = FaultIndeterminate
	}

	// A pattern present across the whole organization is not this server's
	// configuration, whatever its own outcomes suggest.
	if attribution.Fault == FaultGramConfiguration && scope == FaultScopeOrganizationWide {
		attribution.Fault = FaultIndeterminate
	}
	return attribution
}

// compareScope answers whether this server fails materially more often than the
// rest of the organization. It is computed server-side: an external caller has
// no way to divide two numbers it was never given.
//
// The organization totals span every project in scope, so they include this
// server's own calls. Comparing against them directly makes the comparison
// self-cancelling wherever this server is most of the organization's traffic:
// the two rates converge, every pattern reads organization_wide, and a genuine
// single-server fault is reported as a fault of everything. The comparison is
// therefore taken against the organization minus this server.
func compareScope(server, organization outcomeTotals) FaultScope {
	others := organization.without(server)
	if others.Total < organizationWideComparisonFloor || server.Total == 0 {
		return FaultScopeUnknown
	}
	serverRate := float64(server.failures()) / float64(server.Total)
	othersRate := float64(others.failures()) / float64(others.Total)
	if serverRate-othersRate > organizationWideFailureMargin {
		return FaultScopeServerSpecific
	}
	return FaultScopeOrganizationWide
}
