// Package platformswitches stores staff-controlled switches that apply to the
// whole platform rather than to one organization.
package platformswitches

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/platformswitches/repo"
)

// Key names one platform switch.
type Key string

// KeySelfServeDisabled, when engaged, stops every self-serve path from creating
// an organization or arming a self-serve trial. Existing organizations and
// staff-created organizations are unaffected.
const KeySelfServeDisabled Key = "self_serve_disabled"

// State is the current position of a switch.
type State struct {
	Engaged bool
	// UpdatedBy is the staff email that last changed the switch, empty if it
	// was never set.
	UpdatedBy string
	// UpdatedAt is zero if the switch was never set.
	UpdatedAt time.Time
}

// Get returns the switch state. A switch that was never set is off.
func Get(ctx context.Context, db repo.DBTX, key Key) (State, error) {
	row, err := repo.New(db).GetPlatformSwitch(ctx, string(key))
	if errors.Is(err, pgx.ErrNoRows) {
		return State{Engaged: false, UpdatedBy: "", UpdatedAt: time.Time{}}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("get platform switch %s: %w", key, err)
	}
	return stateFromRow(row), nil
}

// Set moves the switch and records who moved it.
func Set(ctx context.Context, db repo.DBTX, key Key, engaged bool, updatedBy string) (State, error) {
	row, err := repo.New(db).SetPlatformSwitch(ctx, repo.SetPlatformSwitchParams{
		Key:       string(key),
		Engaged:   engaged,
		UpdatedBy: conv.ToPGText(updatedBy),
	})
	if err != nil {
		return State{}, fmt.Errorf("set platform switch %s: %w", key, err)
	}
	return stateFromRow(row), nil
}

func stateFromRow(row repo.PlatformSwitch) State {
	return State{
		Engaged:   row.Engaged,
		UpdatedBy: conv.FromPGTextOrEmpty[string](row.UpdatedBy),
		UpdatedAt: row.UpdatedAt.Time,
	}
}
