package pki

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// MaxCertificates bounds the number of stable certificate slots per source.
const MaxCertificates = 64

// Source loads a complete, ordered bundle of parsed certificates. It is called
// only at the observation cadence, never during metric collection. Implementations
// must honor cancellation and return errors safe to log, without PEM or key data.
type Source func(context.Context) ([]*x509.Certificate, error)

// Option configures a WatchDog before it starts.
type Option func(*WatchDog)

// WithSource adds a uniquely named source. Certificate labels use this name and
// zero-based bundle position: rotation preserves labels; reordering changes slots.
func WithSource(name string, source Source) Option {
	return func(w *WatchDog) {
		w.sources = append(w.sources, namedSource{name: name, load: source})
	}
}

// WithObservationInterval overrides the default one-minute source loading cadence.
func WithObservationInterval(interval time.Duration) Option {
	return func(w *WatchDog) { w.interval = interval }
}

type namedSource struct {
	name string
	load Source
}

type certificate struct {
	notBefore  time.Time
	notAfter   time.Time
	attributes metric.ObserveOption
}

type observation struct {
	attributes   metric.ObserveOption
	certificates []certificate
	success      int64
}

// WatchDog observes certificate validity independently of certificate consumers.
// It borrows its meter provider; the caller retains responsibility for shutting
// down the provider after Shutdown returns. A WatchDog can only be started once.
type WatchDog struct {
	logger   *slog.Logger
	provider metric.MeterProvider
	sources  []namedSource
	interval time.Duration

	mu           sync.Mutex
	started      bool
	stopped      bool
	cancel       context.CancelFunc
	done         chan struct{}
	registration metric.Registration

	snapshotMu   sync.RWMutex
	observations []observation
}

// NewWatchDog constructs a watchdog with parsed-certificate sources.
func NewWatchDog(logger *slog.Logger, provider metric.MeterProvider, options ...Option) (*WatchDog, error) {
	w := &WatchDog{
		logger: logger, provider: provider, sources: nil, interval: time.Minute,
		mu: sync.Mutex{}, started: false, stopped: false, cancel: nil,
		done: make(chan struct{}), registration: nil,
		snapshotMu: sync.RWMutex{}, observations: nil,
	}
	for _, option := range options {
		option(w)
	}
	if w.interval <= 0 {
		return nil, errors.New("PKI observation interval must be positive")
	}
	if len(w.sources) == 0 {
		return nil, errors.New("at least one PKI source is required")
	}
	names := make(map[string]bool, len(w.sources))
	for _, source := range w.sources {
		if source.name == "" || len(source.name) > 128 || strings.ContainsAny(source.name, " \t\r\n") || source.load == nil {
			return nil, errors.New("PKI sources require a loader and a whitespace-free name of 1 to 128 bytes")
		}
		if names[source.name] {
			return nil, errors.New("PKI source names must be unique")
		}
		names[source.name] = true
	}
	return w, nil
}

// Start registers metrics and launches an immediate observation followed by
// periodic observations. It returns without waiting for source I/O.
func (w *WatchDog) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started || w.stopped {
		return errors.New("PKI watchdog cannot be started more than once or after shutdown")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start PKI watchdog: %w", err)
	}
	registration, err := w.register()
	if err != nil {
		return err
	}
	w.registration = registration
	ctx, w.cancel = context.WithCancel(ctx)
	w.started = true
	go func() {
		defer close(w.done)
		w.observe(ctx)
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.observe(ctx)
			}
		}
	}()
	return nil
}

// Shutdown cancels source loading, waits for observation to stop, flushes the
// provider when supported, and unregisters the metric callback. It does not shut
// down the borrowed provider. If waiting times out, a subsequent call can finish
// cleanup. Repeated calls after cleanup are harmless.
func (w *WatchDog) Shutdown(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	if !w.started || w.registration == nil {
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
	case <-ctx.Done():
		return fmt.Errorf("stop PKI observation: %w", ctx.Err())
	}
	var err error
	if provider, ok := w.provider.(interface{ ForceFlush(context.Context) error }); ok {
		if flushErr := provider.ForceFlush(ctx); flushErr != nil {
			err = fmt.Errorf("flush PKI metrics: %w", flushErr)
		}
	}
	if unregisterErr := w.registration.Unregister(); unregisterErr != nil {
		err = errors.Join(err, fmt.Errorf("unregister PKI metrics: %w", unregisterErr))
	}
	w.registration = nil
	return err
}

func (w *WatchDog) observe(ctx context.Context) {
	observations := make([]observation, len(w.sources))
	for i, source := range w.sources {
		if ctx.Err() != nil {
			return
		}
		certs, err := source.load(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil && (len(certs) == 0 || len(certs) > MaxCertificates) {
			err = errors.New("certificate source must contain 1 to 64 certificates")
		}
		var certificates []certificate
		if err == nil {
			certificates = make([]certificate, len(certs))
			for index, cert := range certs {
				if cert == nil {
					err = errors.New("certificate source contains a nil certificate")
					break
				}
				certificates[index] = certificate{
					notBefore: cert.NotBefore, notAfter: cert.NotAfter,
					attributes: metric.WithAttributes(attribute.String("pki.source.name", source.name), attribute.Int("pki.certificate.index", index)),
				}
			}
		}
		var success int64 = 1
		if err != nil {
			success = 0
			certificates = nil
			w.logger.ErrorContext(ctx, "PKI source observation failed", attr.SlogError(fmt.Errorf("source %q: %w", source.name, err)))
		}
		observations[i] = observation{attributes: metric.WithAttributes(attribute.String("pki.source.name", source.name)), certificates: certificates, success: success}
	}
	w.snapshotMu.Lock()
	w.observations = observations
	w.snapshotMu.Unlock()
}

func (w *WatchDog) register() (metric.Registration, error) {
	meter := w.provider.Meter("github.com/speakeasy-api/gram/server/pki-watchdog")
	age, err := meter.Float64ObservableGauge("pki.certificate.age", metric.WithUnit("s"), metric.WithDescription("Seconds since NotBefore; negative means not yet valid"))
	if err != nil {
		return nil, fmt.Errorf("create certificate age gauge: %w", err)
	}
	remaining, err := meter.Float64ObservableGauge("pki.certificate.remaining_validity", metric.WithUnit("s"), metric.WithDescription("Seconds until NotAfter; negative means expired"))
	if err != nil {
		return nil, fmt.Errorf("create certificate remaining validity gauge: %w", err)
	}
	success, err := meter.Int64ObservableGauge("pki.source.observation_success", metric.WithUnit("1"), metric.WithDescription("Latest source read and full bundle parse succeeded (1) or failed (0)"))
	if err != nil {
		return nil, fmt.Errorf("create source success gauge: %w", err)
	}
	registration, err := meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		now := time.Now()
		w.snapshotMu.RLock()
		defer w.snapshotMu.RUnlock()
		for _, observation := range w.observations {
			observer.ObserveInt64(success, observation.success, observation.attributes)
			for _, cert := range observation.certificates {
				observer.ObserveFloat64(age, now.Sub(cert.notBefore).Seconds(), cert.attributes)
				observer.ObserveFloat64(remaining, cert.notAfter.Sub(now).Seconds(), cert.attributes)
			}
		}
		return nil
	}, age, remaining, success)
	if err != nil {
		return nil, fmt.Errorf("register PKI observation callback: %w", err)
	}
	return registration, nil
}
