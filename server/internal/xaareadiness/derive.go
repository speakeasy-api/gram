// Package xaareadiness derives, per MCP server, how far an organization is
// from Cross App Access working through its identity provider connection.
// Okta exposes no per-resource connection state to a service app, so the
// model combines what the server's authorization server advertises, what the
// administrator confirmed, and what the exchange path later observed.
// Readiness is advisory: the exchange path never reads it and nothing here
// suppresses a consent screen.
package xaareadiness

import (
	"slices"

	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

// State is the derived readiness of one server, in precedence order.
type State string

const (
	StateNotApplicable   State = "not_applicable"
	StateNeedsAgent      State = "needs_agent"
	StateNeedsConnection State = "needs_connection"
	StateConnected       State = "connected"
)

// Why a server is not applicable.
const ReasonNoIDJAG = "no_idjag"

// Inputs are the facts the derivation consumes; none of them is a state.
type Inputs struct {
	AdvertisesIDJAG bool
	AgentRecorded   bool
	Confirmed       bool
}

// Derive applies the precedence top-down. Connected is the administrator's
// word; whether the exchange works is learned by the exchange path.
func Derive(in Inputs) State {
	if NotApplicableReason(in) != "" {
		return StateNotApplicable
	}
	if !in.AgentRecorded {
		return StateNeedsAgent
	}
	if !in.Confirmed {
		return StateNeedsConnection
	}
	return StateConnected
}

// NotApplicableReason is empty when the server can take part at all.
func NotApplicableReason(in Inputs) string {
	if !in.AdvertisesIDJAG {
		return ReasonNoIDJAG
	}
	return ""
}

// Pending reports whether the administrator still has something to do.
func (s State) Pending() bool {
	switch s {
	case StateNeedsAgent, StateNeedsConnection:
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
