package gram

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	pkiMaxCertificates = 64
	pkiMaxPEMBytes     = 1 << 20
)

type pkiSource struct {
	name        string
	location    string
	environment bool
}

type pkiCertificate struct {
	notBefore  time.Time
	notAfter   time.Time
	attributes metric.ObserveOption
}

type pkiObservation struct {
	attributes   metric.ObserveOption
	certificates []pkiCertificate
	success      int64
}

type pkiWatchdog struct {
	mu           sync.RWMutex
	sources      []pkiSource
	observations []pkiObservation
}

func newPKIWatchdogCommand() *cli.Command {
	return &cli.Command{
		Name:        "pki-watchdog",
		Usage:       "Observe named public PEM certificate bundles over OTLP without application dependencies",
		Description: "Each source emits observation success. Valid bundles emit age and remaining validity in seconds for every certificate, labeled by source name and zero-based PEM order (maximum 64). Reordering changes slot identity; rotation in place preserves labels. Invalid bundles emit no lifetime gauges. Files are reopened every observation; environment values are process-local and require a restart for external updates. No chain or hostname verification is performed.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "certificate-file", Usage: "Named PEM file: NAME=PATH (repeatable; comma-separated in env)", EnvVars: []string{"GRAM_PKI_CERTIFICATE_FILES"}},
			&cli.StringSliceFlag{Name: "certificate-env", Usage: "Named PEM environment source: NAME=ENV_VAR (repeatable; comma-separated in env)", EnvVars: []string{"GRAM_PKI_CERTIFICATE_ENVS"}},
			&cli.DurationFlag{Name: "observation-interval", Value: time.Minute, Usage: "Certificate reread cadence; OTLP export uses the shared 60-second cadence", EnvVars: []string{"GRAM_PKI_OBSERVATION_INTERVAL"}},
		},
		Action: func(c *cli.Context) (err error) {
			sources, err := pkiSources(c.StringSlice("certificate-file"), c.StringSlice("certificate-env"))
			if err != nil {
				return err
			}
			interval := c.Duration("observation-interval")
			if interval <= 0 {
				return errors.New("observation-interval must be positive")
			}
			const serviceName = "gram-pki-watchdog"
			logger := PullLogger(c.Context).With(attr.SlogComponent("pki_watchdog"), attr.SlogServiceName(serviceName), attr.SlogServiceVersion(shortGitSHA()))
			ctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer stop()
			shutdownOTel, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName: serviceName, ServiceVersion: shortGitSHA(), GitSHA: GitSHA, EnableTracing: false, EnableMetrics: true,
			})
			if err != nil {
				return fmt.Errorf("setup PKI telemetry: %w", err)
			}
			provider := otel.GetMeterProvider()
			var registration metric.Registration
			// This metrics-only command shuts down the provider, which drains
			// the reader and closes its exporter. The shared setup's cleanup
			// closes the same exporter directly and must not run a second time.
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer cancel()
				if managed, ok := provider.(interface {
					ForceFlush(context.Context) error
					Shutdown(context.Context) error
				}); ok {
					err = errors.Join(err, managed.ForceFlush(shutdownCtx), managed.Shutdown(shutdownCtx))
				} else {
					err = errors.Join(err, shutdownOTel(shutdownCtx))
				}
				if registration != nil {
					err = errors.Join(err, registration.Unregister())
				}
			}()
			watchdog := &pkiWatchdog{mu: sync.RWMutex{}, sources: sources, observations: nil}
			watchdog.observe(ctx, logger)
			registration, err = watchdog.register(provider)
			if err != nil {
				return err
			}
			logger.InfoContext(ctx, "PKI watchdog started")
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					watchdog.observe(ctx, logger)
				}
			}
		},
	}
}

func pkiSources(files, envs []string) ([]pkiSource, error) {
	sources := make([]pkiSource, 0, len(files)+len(envs))
	names := make(map[string]bool, len(files)+len(envs))
	for kind, entries := range [][]string{files, envs} {
		for _, entry := range entries {
			name, location, ok := strings.Cut(entry, "=")
			if !ok || name == "" || location == "" || len(name) > 128 || strings.ContainsAny(name, " \t\r\n") {
				return nil, errors.New("certificate sources must be NAME=SOURCE with a nonempty whitespace-free name of at most 128 bytes")
			}
			if names[name] {
				return nil, errors.New("certificate source names must be unique")
			}
			names[name] = true
			sources = append(sources, pkiSource{name: name, location: location, environment: kind == 1})
		}
	}
	if len(sources) == 0 {
		return nil, errors.New("at least one certificate-file or certificate-env is required")
	}
	return sources, nil
}

func readPKICertificates(source pkiSource) ([]pkiCertificate, error) {
	var data []byte
	if source.environment {
		value, ok := os.LookupEnv(source.location)
		if !ok {
			return nil, errors.New("certificate environment variable is unset")
		}
		if len(value) > pkiMaxPEMBytes {
			return nil, errors.New("certificate source exceeds 1 MiB")
		}
		data = []byte(value)
	} else {
		file, err := os.Open(source.location)
		if err != nil {
			return nil, errors.New("cannot open certificate file")
		}
		defer o11y.NoLogDefer(file.Close)
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("certificate source must be a regular file")
		}
		data, err = io.ReadAll(io.LimitReader(file, pkiMaxPEMBytes+1))
		if err != nil {
			return nil, errors.New("cannot read certificate file")
		}
		if len(data) > pkiMaxPEMBytes {
			return nil, errors.New("certificate source exceeds 1 MiB")
		}
	}
	var certificates []pkiCertificate
	for len(bytes.TrimSpace(data)) > 0 {
		data = bytes.TrimSpace(data)
		// pem.Decode skips malformed leading blocks. Require the next complete
		// block to start here, so a damaged bundle cannot appear successful.
		const begin = "-----BEGIN CERTIFICATE-----"
		const end = "-----END CERTIFICATE-----"
		if !bytes.HasPrefix(data, []byte(begin)) {
			return nil, errors.New("expected certificate PEM block")
		}
		endIndex := bytes.Index(data, []byte(end))
		if endIndex < 0 {
			return nil, errors.New("unterminated certificate PEM block")
		}
		blockEnd := endIndex + len(end)
		block, rest := pem.Decode(data[:blockEnd])
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(rest) != 0 || bytes.Contains(data[len(begin):endIndex], []byte("-----BEGIN")) {
			return nil, errors.New("invalid certificate PEM block")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid X.509 certificate")
		}
		if len(certificates) == pkiMaxCertificates {
			return nil, errors.New("certificate bundle exceeds 64 certificates")
		}
		certificates = append(certificates, pkiCertificate{
			notBefore: cert.NotBefore, notAfter: cert.NotAfter,
			attributes: metric.WithAttributes(attribute.String("pki.source.name", source.name), attribute.Int("pki.certificate.index", len(certificates))),
		})
		data = data[blockEnd:]
	}
	if len(certificates) == 0 {
		return nil, errors.New("certificate bundle is empty")
	}
	return certificates, nil
}

func (w *pkiWatchdog) observe(ctx context.Context, logger *slog.Logger) {
	observations := make([]pkiObservation, len(w.sources))
	for i, source := range w.sources {
		if ctx.Err() != nil {
			return
		}
		certificates, err := readPKICertificates(source)
		var success int64 = 1
		if err != nil {
			success = 0
			logger.ErrorContext(ctx, "PKI source observation failed", attr.SlogError(fmt.Errorf("source %q: %w", source.name, err)))
		}
		observations[i] = pkiObservation{attributes: metric.WithAttributes(attribute.String("pki.source.name", source.name)), certificates: certificates, success: success}
	}
	w.mu.Lock()
	w.observations = observations
	w.mu.Unlock()
}

func (w *pkiWatchdog) register(provider metric.MeterProvider) (metric.Registration, error) {
	meter := provider.Meter("github.com/speakeasy-api/gram/server/pki-watchdog")
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
		w.mu.RLock()
		defer w.mu.RUnlock()
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
