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

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const (
	RuntimeProviderLocal = runtimeBackendLocal
	RuntimeProviderFlyIO = runtimeBackendFlyIO
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

func normalizeRuntimeProvider(provider string) string {
	switch provider {
	case "", runtimeBackendLegacyFirecracker:
		return RuntimeProviderLocal
	default:
		return provider
	}
}

// NewRuntimeBackend selects the runtime implementation. The provider string
// is validated by assistantRuntimeConfigFromCLI before reaching here, so any
// unknown value is a programmer error and panics rather than silently
// degrading.
func NewRuntimeBackend(logger *slog.Logger, httpPolicy *guardian.Policy, config RuntimeBackendConfig) RuntimeBackend {
	provider := normalizeRuntimeProvider(config.Provider)
	if provider != RuntimeProviderLocal {
		panic(fmt.Sprintf("assistants.NewRuntimeBackend: unsupported provider %q (CLI validation should have rejected this)", provider))
	}
	return NewRuntimeManager(logger, httpPolicy, config.Local)
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
