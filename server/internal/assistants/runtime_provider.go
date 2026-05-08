package assistants

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/superfly/fly-go/tokens"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	RuntimeProviderLocal = runtimeBackendLocal
	RuntimeProviderFlyIO = runtimeBackendFlyIO

	defaultFlyRuntimeRegion = "us"
	defaultFlyRuntimePrefix = "gram-asst"
)

type RuntimeBackendConfig struct {
	Provider string
	Local    RuntimeManagerConfig
	Fly      FlyRuntimeConfig
}

type FlyRuntimeConfig struct {
	ServiceName        string
	ServiceVersion     string
	FlyTokens          *tokens.Tokens
	FlyAPIURL          string
	FlyMachinesBaseURL string
	DefaultFlyOrg      string
	DefaultFlyRegion   string
	OCIImage           string
	ImageVersion       string
	AppNamePrefix      string
	ServerURLOverride  *url.URL
}

func (c FlyRuntimeConfig) Validate() error {
	if c.FlyTokens == nil {
		return fmt.Errorf("--assistant-runtime-flyio-api-token is required")
	}
	if c.DefaultFlyOrg == "" {
		return fmt.Errorf("--assistant-runtime-flyio-org is required")
	}
	if c.OCIImage == "" {
		return fmt.Errorf("--assistant-runtime-oci-image is required")
	}
	if c.ImageVersion == "" {
		return fmt.Errorf("--assistant-runtime-image-version is required")
	}
	return nil
}

func normalizeRuntimeProvider(provider string) string {
	switch provider {
	case "", runtimeBackendLegacyFirecracker:
		return RuntimeProviderLocal
	default:
		return provider
	}
}

func NewRuntimeBackend(logger *slog.Logger, tracerProvider trace.TracerProvider, httpPolicy *guardian.Policy, config RuntimeBackendConfig) RuntimeBackend {
	provider := normalizeRuntimeProvider(config.Provider)
	switch provider {
	case RuntimeProviderLocal:
		return NewRuntimeManager(logger, httpPolicy, config.Local)
	case RuntimeProviderFlyIO:
		return NewFlyRuntimeBackend(logger, tracerProvider, httpPolicy, config.Fly)
	default:
		panic(fmt.Sprintf("assistants.NewRuntimeBackend: unsupported provider %q (CLI validation should have rejected this)", provider))
	}
}

func ValidateRuntimeBackendServerURL(ctx context.Context, runtime RuntimeBackend, serverURL *url.URL) error {
	if runtime == nil || runtime.Backend() != runtimeBackendFlyIO {
		return nil
	}
	_, err := runtime.ServerURL(ctx, assistantRuntimeRecord{
		ID:                  uuid.Nil,
		AssistantThreadID:   uuid.Nil,
		AssistantID:         uuid.Nil,
		ProjectID:           uuid.Nil,
		Backend:             runtime.Backend(),
		BackendMetadataJSON: nil,
		State:               "",
		WarmUntil: pgtype.Timestamptz{
			Time:             time.Time{},
			InfinityModifier: pgtype.Finite,
			Valid:            false,
		},
	}, serverURL)
	if err != nil {
		return fmt.Errorf("validate assistant runtime server url: %w", err)
	}
	return nil
}
