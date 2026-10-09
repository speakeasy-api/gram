package otelpub

import (
	"context"
	"fmt"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// processor exports each record on the emitting goroutine. Unlike
// sdklog.SimpleProcessor it holds no lock, so concurrent emits publish in
// parallel.
type processor struct {
	exporter sdklog.Exporter
}

var _ sdklog.Processor = processor{exporter: nil}

func (processor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (p processor) OnEmit(ctx context.Context, record *sdklog.Record) error {
	if err := p.exporter.Export(ctx, []sdklog.Record{*record}); err != nil {
		return fmt.Errorf("export log record: %w", err)
	}
	return nil
}

func (p processor) Shutdown(ctx context.Context) error {
	if err := p.exporter.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down log exporter: %w", err)
	}
	return nil
}

func (p processor) ForceFlush(ctx context.Context) error {
	if err := p.exporter.ForceFlush(ctx); err != nil {
		return fmt.Errorf("flush log exporter: %w", err)
	}
	return nil
}
