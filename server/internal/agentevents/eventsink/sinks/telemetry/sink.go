package telemetry

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/eventsink"
	gramtelemetry "github.com/speakeasy-api/gram/server/internal/telemetry"
)

type Logger interface {
	Log(ctx context.Context, params gramtelemetry.LogParams)
}

type Sink[T any] struct {
	Logger Logger
}

func New[T any](logger Logger) *Sink[T] {
	return &Sink[T]{
		Logger: logger,
	}
}

func (s *Sink[T]) Write(ctx context.Context, ev agentevents.Event[T]) error {
	if s == nil || s.Logger == nil {
		return nil
	}

	source := string(ev.Provider())
	logs, err := eventsink.BuildTelemetryLogs(ev, source)
	if err != nil {
		return err
	}
	for _, params := range logs {
		s.Logger.Log(ctx, params)
	}
	return nil
}
