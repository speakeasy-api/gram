package testenv

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type PresidioClientFunc func(t *testing.T) *risk_analysis.PresidioClient

func NewTestPresidio(ctx context.Context) (testcontainers.Container, PresidioClientFunc, error) {
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "mcr.microsoft.com/presidio-analyzer:2.2.362",
			ExposedPorts: []string{"3000/tcp"},
			WaitingFor:   wait.ForHTTP("/health").WithPort("3000/tcp").WithStartupTimeout(120 * time.Second),
		},
		Started: true,
		Logger:  NewTestcontainersLogger(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("start presidio container: %w", err)
	}

	return container, newPresidioClientFunc(container), nil
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
			&http.Client{Timeout: 30 * time.Second},
			NewTracerProvider(t),
			NewMeterProvider(t),
			NewLogger(t),
		)
	}
}
