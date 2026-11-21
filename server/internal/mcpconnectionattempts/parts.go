package mcpconnectionattempts

import (
	"slices"
	"time"
)

// Part is the part of a connection a failure broke. A connection is a chain: the app
// signs in to Speakeasy, the person signs in to the service, the server answers. A
// later success ends a failure only when it got at least as far along the chain.
type Part string

const (
	// PartAppSignIn is the app's sign-in to Speakeasy, held by one person's app on one
	// server. Only that app's next sign-in, refresh or served request ends it.
	PartAppSignIn Part = "app_sign_in"

	// PartServiceSignIn is the person's sign-in to the outside service. Their next
	// successful sign-in or refresh with it ends it, or the server next answering them.
	PartServiceSignIn Part = "service_sign_in"

	// PartServer is the server, gateway member or toolset answering. It is not about
	// the person, so it answering anyone next ends it.
	PartServer Part = "server"
)

// allParts are every Part.
var allParts = []Part{PartAppSignIn, PartServiceSignIn, PartServer}

// serverDidNotAnswer are the request and listing reasons where the server, or a check
// every request goes through, did not answer whoever asked. Every other reason there
// can be one caller's own, so another caller's success proves nothing about it.
var serverDidNotAnswer = []string{
	CodeKillswitchCheckUnavailable.name,
	CodeProviderUnreachable.name,
	CodeProviderUnavailable.name,
	CodeProviderServerError.name,
	CodeTunnelUnavailable.name,
	CodeTunnelAtCapacity.name,
}

// PartOf returns the part a failure broke, from the step it was recorded on. A request
// or listing can be refused for any part, so there the reason decides. An unknown
// reason on those steps is the caller's own, which only their own success ends.
func PartOf(step, reason string) Part {
	switch step {
	case StepProviderSignIn.name, StepProviderToken.name, StepProviderRefresh.name, StepUpstreamEMA.name:
		return PartServiceSignIn
	case StepMCPRequest.name, StepDiscoveryListing.name, StepDiscoveryMember.name:
		switch {
		case reason == CodeInvalidBearerToken.name:
			return PartAppSignIn
		case slices.Contains(serverDidNotAnswer, reason):
			return PartServer
		default:
			return PartServiceSignIn
		}
	default:
		return PartAppSignIn
	}
}

// didNotFinish are failed reasons that mean the person did not finish signing in, or
// that a newer sign-in took over. Neither is a broken connection.
var didNotFinish = []string{CodeExpired.name, CodeLoginExpired.name}

// CountsAsFailing reports whether an ending is a broken connection. Blocked, declined
// and unfinished attempts never are.
func (e Ending) CountsAsFailing() bool {
	return e.Outcome == OutcomeFailed && !slices.Contains(didNotFinish, e.Reason)
}

const (
	// GraceRetry is how long a failure the person or app gets past may last before it
	// counts: apps retry and people finish signing in within minutes.
	GraceRetry = 10 * time.Minute

	// GraceOutage is how long an outage of a service or of Speakeasy may last before it
	// counts, so a brief blip is not reported.
	GraceOutage = 30 * time.Minute

	// GraceNone is for what an admin has to fix, which does not fix itself.
	GraceNone = time.Duration(0)
)

// GraceOf is how long a run of failures may last before the person counts as failing,
// from who the catalog says fixes its latest reason.
func GraceOf(reason string) time.Duration {
	meaning, _ := MeaningOf(reason)
	switch meaning.WhoFixes {
	case WhoAdmin:
		return GraceNone
	case WhoProvider, WhoSpeakeasy:
		return GraceOutage
	case WhoPerson, WhoApp, WhoNobody:
		return GraceRetry
	default:
		return GraceRetry
	}
}
