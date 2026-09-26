package gram

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/pki"
)

const (
	pkiMaxPEMBytes = 1 << 20
)

type pkiSource struct {
	location    string
	environment bool
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
			var watchdog *pki.WatchDog
			// This metrics-only command shuts down the provider, which drains
			// the reader and closes its exporter. The shared setup's cleanup
			// closes the same exporter directly and must not run a second time.
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer cancel()
				if watchdog != nil {
					err = errors.Join(err, watchdog.Shutdown(shutdownCtx))
				}
				if managed, ok := provider.(interface {
					Shutdown(context.Context) error
				}); ok {
					err = errors.Join(err, managed.Shutdown(shutdownCtx))
				} else {
					err = errors.Join(err, shutdownOTel(shutdownCtx))
				}
			}()
			watchdog, err = pki.NewWatchDog(logger, provider, append(sources, pki.WithObservationInterval(interval))...)
			if err != nil {
				return fmt.Errorf("configure PKI watchdog: %w", err)
			}
			if err := watchdog.Start(ctx); err != nil {
				return fmt.Errorf("start PKI watchdog: %w", err)
			}
			logger.InfoContext(ctx, "PKI watchdog started")
			<-ctx.Done()
			return nil
		},
	}
}

func pkiSources(files, envs []string) ([]pki.Option, error) {
	options := make([]pki.Option, 0, len(files)+len(envs))
	for kind, entries := range [][]string{files, envs} {
		for _, entry := range entries {
			name, location, ok := strings.Cut(entry, "=")
			if !ok || location == "" {
				return nil, errors.New("certificate sources must be NAME=SOURCE with a nonempty source")
			}
			source := pkiSource{location: location, environment: kind == 1}
			options = append(options, pki.WithSource(name, func(ctx context.Context) ([]*x509.Certificate, error) {
				return readPKICertificates(ctx, source)
			}))
		}
	}
	return options, nil
}

func readPKICertificates(ctx context.Context, source pkiSource) ([]*x509.Certificate, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read PKI source: %w", err)
	}
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
	var certificates []*x509.Certificate
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
		if len(certificates) == pki.MaxCertificates {
			return nil, errors.New("certificate bundle exceeds 64 certificates")
		}
		certificates = append(certificates, cert)
		data = data[blockEnd:]
	}
	if len(certificates) == 0 {
		return nil, errors.New("certificate bundle is empty")
	}
	return certificates, nil
}
