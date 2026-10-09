package otelpub

import (
	"context"
	"fmt"
	"sync"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// processor exports each record on the emitting goroutine. Emits share a
// read lock, so they publish in parallel; Shutdown takes the write lock, so
// it waits for in-flight emits and later ones are dropped.
type processor struct {
	exporter sdklog.Exporter
	mu       sync.RWMutex
	stopped  bool
}

var _ sdklog.Processor = (*processor)(nil)

func newProcessor(exporter sdklog.Exporter) *processor {
	return &processor{exporter: exporter, mu: sync.RWMutex{}, stopped: false}
}

func (*processor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }

func (p *processor) OnEmit(ctx context.Context, record *sdklog.Record) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.stopped {
		return nil
	}
	if err := p.exporter.Export(ctx, []sdklog.Record{*record}); err != nil {
		return fmt.Errorf("export log record: %w", err)
	}
	return nil
}

func (p *processor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
	if err := p.exporter.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down log exporter: %w", err)
	}
	return nil
}

func (p *processor) ForceFlush(ctx context.Context) error {
	if err := p.exporter.ForceFlush(ctx); err != nil {
		return fmt.Errorf("flush log exporter: %w", err)
	}
	return nil
}
