// Package oktaresourceconnections derives, per MCP server, how far an organization is
// from Cross App Access working through its identity provider connection.
// Okta exposes no per-resource connection state to a service app, so the
// model combines what the server's authorization server advertises, what the
// administrator confirmed, and what the exchange path later observed.
// Readiness is advisory: the exchange path never reads it and nothing here
// suppresses a consent screen.
package oktaresourceconnections

import (
	"slices"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

// State is the derived readiness of one server, in precedence order.
type State string

const (
	StateNotApplicable   State = "not_applicable"
	StateNeedsAgent      State = "needs_agent"
	StateNeedsConnection State = "needs_connection"
	StateBroken          State = "broken"
	StateConnected       State = "connected"
	StateVerified        State = "verified"
)

// Why a server is not applicable.
const ReasonNoIDJAG = "no_idjag"

// Result is what an identity chaining exchange showed about an upstream's
// resource connection.
type Result string

const (
	ResultVerified           Result = "verified"
	ResultDownstreamRejected Result = "downstream_rejected"
	ResultConnectionMissing  Result = "connection_missing"
	ResultScopeNotAllowed    Result = "scope_not_allowed"
	ResultClientAuthFailed   Result = "client_auth_failed"
)

// Inputs are the facts the derivation consumes; none of them is a state.
type Inputs struct {
	AdvertisesIDJAG bool
	AgentRecorded   bool
	Confirmed       bool

	// Observed is the latest exchange result since confirmation, if any.
	Observed Result
}

// Derive applies the precedence top-down. Connected is the administrator's
// word; verified is the exchange path's.
func Derive(in Inputs) State {
	if NotApplicableReason(in) != "" {
		return StateNotApplicable
	}
	if !in.AgentRecorded {
		return StateNeedsAgent
	}
	switch {
	case !in.Confirmed:
		return StateNeedsConnection
	case in.Observed == ResultVerified:
		return StateVerified
	case in.Observed == ResultConnectionMissing:
		return StateNeedsConnection
	case BrokenReason(in) != "":
		return StateBroken
	default:
		return StateConnected
	}
}

// NotApplicableReason is empty when the server can take part at all.
func NotApplicableReason(in Inputs) string {
	if !in.AdvertisesIDJAG {
		return ReasonNoIDJAG
	}
	return ""
}

// BrokenReason is empty unless a confirmed connection is known not to work.
func BrokenReason(in Inputs) string {
	if !in.Confirmed {
		return ""
	}
	switch in.Observed {
	case ResultDownstreamRejected, ResultScopeNotAllowed, ResultClientAuthFailed:
		return string(in.Observed)
	case ResultVerified, ResultConnectionMissing, "":
		return ""
	}
	return ""
}

// Pending reports whether the administrator still has something to do.
func (s State) Pending() bool {
	switch s {
	case StateNeedsAgent, StateNeedsConnection, StateBroken:
		return true
	default:
		return false
	}
}

// AdvertisesIDJAG requires both the profile and the jwt-bearer grant type,
// since jwt-bearer alone covers other assertion profiles.
func AdvertisesIDJAG(grantTypes, grantProfiles []string) bool {
	return slices.Contains(grantProfiles, oauthwire.GrantProfileIDJAG) &&
		slices.Contains(grantTypes, oauthwire.GrantTypeJWTBearer)
}
