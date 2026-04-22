package assistants

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/superfly/fly-go/tokens"
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

func NewRuntimeBackend(logger *slog.Logger, config RuntimeBackendConfig) (RuntimeBackend, error) {
	switch normalizeRuntimeProvider(config.Provider) {
	case RuntimeProviderLocal:
		return NewRuntimeManager(logger, config.Local), nil
	default:
		return nil, fmt.Errorf("unsupported assistant runtime provider %q", config.Provider)
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
		WarmUntil: sql.NullTime{
			Time:  time.Time{},
			Valid: false,
		},
	}, serverURL)
	if err != nil {
		return fmt.Errorf("validate assistant runtime server url: %w", err)
	}
	return nil
}
