package tunneledmcp

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/publiclimits"
)

// validatePublicRateLimits rejects a limit outside [0, max]. 0 is the clear
// sentinel; the column CHECK constraints refuse it as a stored value, so it
// never reaches the row as anything but NULL.
func validatePublicRateLimits(payload *gen.UpdateServerPayload) error {
	for _, field := range []struct {
		name  string
		value *int
		max   int
	}{
		{name: "public_request_rate_per_second", value: payload.PublicRequestRatePerSecond, max: publiclimits.MaxRequestRatePerSecond},
		{name: "public_request_burst", value: payload.PublicRequestBurst, max: publiclimits.MaxRequestBurst},
	} {
		if field.value != nil && (*field.value < 0 || *field.value > field.max) {
			return fmt.Errorf("%s must be between 0 and %d", field.name, field.max)
		}
	}
	return nil
}

// optionalPGInt4 maps a tri-state form field to its query parameter: nil is
// "leave unchanged" (NULL), any value (including the 0 clear sentinel) is
// passed through. Callers validate the range first, so the narrowing is safe.
func optionalPGInt4(v *int) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{Int32: 0, Valid: false}
	}
	return pgtype.Int4{Int32: int32(*v), Valid: true} //nolint:gosec // bounded by validatePublicRateLimits
}
