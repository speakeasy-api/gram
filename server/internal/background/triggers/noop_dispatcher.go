package triggers

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type NoopDispatcher struct {
	logger *slog.Logger
}

func NewNoopDispatcher(logger *slog.Logger) *NoopDispatcher {
	if logger != nil {
		logger = logger.With(attr.SlogComponent("background_triggers_noop_dispatcher"))
	}

	return &NoopDispatcher{logger: logger}
}

func (d *NoopDispatcher) Kind() string {
	return TargetKindNoop
}

func (d *NoopDispatcher) Dispatch(ctx context.Context, input Task) error {
	if d.logger != nil {
		d.logger.InfoContext(
			ctx,
			"noop trigger dispatched",
			attr.SlogTriggerInstanceID(input.TriggerInstanceID),
			attr.SlogTriggerDefinitionSlug(input.DefinitionSlug),
			attr.SlogTriggerTargetKind(input.TargetKind),
			attr.SlogTriggerTargetRef(input.TargetRef),
			attr.SlogTriggerEventID(input.EventID),
		)
	}

	return nil
}
