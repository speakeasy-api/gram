// Package publiclimits is the single source of the anonymous public admission
// limits a tunneled MCP server is served under: the deployment defaults, the
// bounds a stored value must satisfy, and how a row's stored values resolve to
// the limit the serve path applies. The MCP serve path and the management API
// both read from here so what a tenant sees is what is enforced.
package publiclimits

import "github.com/jackc/pgx/v5/pgtype"

const (
	// DefaultRequestRatePerSecond is the sustained anonymous MCP request rate
	// admitted per tunnel when the row stores no limit.
	DefaultRequestRatePerSecond = 50
	// DefaultRequestBurst is the token-bucket capacity that pairs with the
	// default rate.
	DefaultRequestBurst = 100

	// MaxRequestRatePerSecond and MaxRequestBurst bound stored values. They
	// mirror the tunneled_mcp_servers CHECK constraints and the Goa design; a
	// change here must land in all three.
	MaxRequestRatePerSecond = 100_000
	MaxRequestBurst         = 1_000_000
)

// Effective resolves a row's stored limit to the rate and burst the serve path
// applies: an unset rate means the deployment defaults; a stored rate without a
// burst admits a burst of twice the rate.
func Effective(rate, burst pgtype.Int4) (perSecond int, burstCap int) {
	if !rate.Valid || rate.Int32 <= 0 {
		return DefaultRequestRatePerSecond, DefaultRequestBurst
	}
	perSecond = int(rate.Int32)
	if burst.Valid && burst.Int32 > 0 {
		return perSecond, int(burst.Int32)
	}
	return perSecond, 2 * perSecond
}

// Stored reports whether the row carries its own limit rather than the
// deployment default.
func Stored(rate pgtype.Int4) bool {
	return rate.Valid && rate.Int32 > 0
}
