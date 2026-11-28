package auth

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/platformswitches"
)

// ErrSelfServeDisabled refuses a self-serve organization because staff turned
// self-serve signup off across the platform.
var ErrSelfServeDisabled = errors.New("self-serve signup is disabled")

const selfServeDisabledMessage = "Self-serve signup is temporarily unavailable. Contact sales@speakeasy.com to get started."

// selfServeDisabled fails open: a read error must not lock every new user out
// while the database is otherwise serving signups.
func (s *Service) selfServeDisabled(ctx context.Context) bool {
	state, err := platformswitches.Get(ctx, s.db, platformswitches.KeySelfServeDisabled)
	if err != nil {
		s.logger.ErrorContext(ctx, "read self-serve switch; allowing self-serve", attr.SlogError(err))
		return false
	}
	return state.Engaged
}
