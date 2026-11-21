package mcpconnectionattempts

import (
	"slices"
	"time"
)

// Part is the part of a connection a failure broke: the app's sign-in to Speakeasy, the
// person's sign-in to the service, or the server answering. Each part's failure ends
// only on evidence of that part working, for the same person unless it is the server.
type Part string

const (
	// PartAppSignIn is the app's sign-in to Speakeasy, held by one person's app on one
	// server. That app's next sign-in, refresh or served request ends it; a failure filed
	// under no verified app is ended by any app's of the same client.
	PartAppSignIn Part = "app_sign_in"

	// PartServiceSignIn is the person's sign-in to the outside service. Their next
	// successful sign-in or refresh with it ends it, or the server next answering them.
	PartServiceSignIn Part = "service_sign_in"

	// PartServer is the server, gateway member or toolset answering. It is not about
	// the person, so it answering anyone next ends it.
	PartServer Part = "server"
)

// serverDidNotAnswer are the request and listing reasons where the server, or a check
// every request goes through, did not answer whoever asked. Every other reason there
// can be one caller's own, so another caller's success proves nothing about it.
var serverDidNotAnswer = []string{
	CodeAuthCheckUnavailable.name,
	CodeProviderUnreachable.name,
	CodeProviderUnavailable.name,
	CodeProviderServerError.name,
	CodeTunnelUnavailable.name,
	CodeTunnelAtCapacity.name,
}

// serviceSignInNeedsRenewing are reasons a refresh is refused for because the person's
// sign-in to the service needs renewing, whatever step records them.
var serviceSignInNeedsRenewing = []string{CodeUpstreamReconnectRequired.name, CodeUpstreamEMARefused.name}

// PartOf returns the part a failure broke, from the step it was recorded on. A request
// or listing can be refused for any part, so there the reason decides. An unknown
// reason on those steps is the caller's own, which only their own success ends.
func PartOf(step, reason string) Part {
	if slices.Contains(serviceSignInNeedsRenewing, reason) {
		return PartServiceSignIn
	}
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
