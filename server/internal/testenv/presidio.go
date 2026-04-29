package testenv

import (
	"context"
	"fmt"
	"log"
	"testing"
	"time"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type PresidioClientFunc func(t *testing.T) *risk_analysis.PresidioClient

func NewTestPresidio(ctx context.Context) (testcontainers.Container, PresidioClientFunc, error) {
	// The presidio-analyzer image loads spaCy NLP models on startup, which is
	// CPU-intensive and can exceed a single timeout on contended CI runners.
	// Retry with a moderate per-attempt timeout so stuck containers fail fast
	// but slow startups get a second chance.
	const maxAttempts = 2
	const startupTimeout = 300 * time.Second

	startedAt := time.Now()
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        "mcr.microsoft.com/presidio-analyzer:2.2.362",
				ExposedPorts: []string{"3000/tcp"},
				WaitingFor: wait.ForHTTP("/health").
					WithPort("3000/tcp").
					WithPollInterval(2 * time.Second).
					WithStartupTimeout(startupTimeout),
			},
			Started: true,
			Logger:  NewTestcontainersLogger(),
		})
		if err == nil {
			log.Printf("presidio container ready in %s (attempt %d/%d)", time.Since(startedAt).Round(time.Millisecond), attempt, maxAttempts)
			return container, newPresidioClientFunc(container), nil
		}

		lastErr = err
		log.Printf("presidio container attempt %d/%d failed after %s: %v", attempt, maxAttempts, time.Since(startedAt).Round(time.Millisecond), err)

		if container != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_ = container.Terminate(cleanupCtx)
			cancel()
		}
	}

	return nil, nil, fmt.Errorf("start presidio container after %d attempts: %w", maxAttempts, lastErr)
}

func newPresidioClientFunc(container testcontainers.Container) PresidioClientFunc {
	return func(t *testing.T) *risk_analysis.PresidioClient {
		t.Helper()

		host, err := container.Host(t.Context())
		if err != nil {
			t.Fatalf("get presidio container host: %v", err)
		}

		port, err := container.MappedPort(t.Context(), "3000/tcp")
		if err != nil {
			t.Fatalf("get presidio container port: %v", err)
		}

		baseURL := fmt.Sprintf("http://%s:%s", host, port.Port())

		return risk_analysis.NewPresidioClient(
			baseURL,
			NewTracerProvider(t),
			NewMeterProvider(t),
			NewLogger(t),
		)
	}
}
