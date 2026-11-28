package admin

import (
	"context"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformswitches"
)

func (s *Service) GetSelfServeStatus(ctx context.Context, _ *gen.GetSelfServeStatusPayload) (*gen.AdminSelfServeStatus, error) {
	state, err := platformswitches.Get(ctx, s.db, platformswitches.KeySelfServeDisabled)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read self-serve status").LogError(ctx, s.logger)
	}
	return selfServeStatusView(state), nil
}

func (s *Service) SetSelfServeDisabled(ctx context.Context, payload *gen.SetSelfServeDisabledPayload) (*gen.AdminSelfServeStatus, error) {
	operator, err := hooksRolloutOperator(ctx)
	if err != nil {
		return nil, err
	}
	state, err := platformswitches.Set(ctx, s.db, platformswitches.KeySelfServeDisabled, payload.Disabled, operator)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set self-serve status").LogError(ctx, s.logger)
	}
	message := "self-serve signup re-enabled across all organizations"
	if payload.Disabled {
		message = "self-serve signup disabled across all organizations"
	}
	s.logger.WarnContext(ctx, message, attr.SlogAuthUserEmail(operator))
	return selfServeStatusView(state), nil
}

func selfServeStatusView(state platformswitches.State) *gen.AdminSelfServeStatus {
	view := &gen.AdminSelfServeStatus{Disabled: state.Engaged, UpdatedBy: nil, UpdatedAt: nil}
	if state.UpdatedBy != "" {
		view.UpdatedBy = &state.UpdatedBy
	}
	if !state.UpdatedAt.IsZero() {
		at := state.UpdatedAt.UTC().Format(time.RFC3339Nano)
		view.UpdatedAt = &at
	}
	return view
}
