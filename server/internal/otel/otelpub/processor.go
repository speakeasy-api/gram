package otelpub

import (
	"context"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// processor exports each record on the emitting goroutine. Unlike
// sdklog.SimpleProcessor it holds no lock, so concurrent emits publish in
// parallel.
type processor struct {
	exporter sdklog.Exporter
}

var _ sdklog.Processor = processor{}

func (processor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (p processor) OnEmit(ctx context.Context, record *sdklog.Record) error {
	return p.exporter.Export(ctx, []sdklog.Record{*record})
}

func (p processor) Shutdown(ctx context.Context) error { return p.exporter.Shutdown(ctx) }

func (p processor) ForceFlush(ctx context.Context) error { return p.exporter.ForceFlush(ctx) }
