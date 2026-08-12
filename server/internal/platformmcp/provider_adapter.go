package platformmcp

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
)

var (
	ErrProviderAdapterUnavailable  = errors.New("platform mcp provider adapter unavailable")
	ErrSetupHandoffReissueRequired = errors.New("platform mcp setup handoff reissue required")
)

// ProviderSetupRequest is created from a trusted, lifecycle-validated handoff.
// It contains identifiers, never a handoff value, OAuth value, provider secret,
// or remote URL. HandoffID is set only after the handoff is consumed.
type ProviderSetupRequest struct {
	UserID              string
	OrganizationID      string
	ProjectID           uuid.UUID
	RegistrationID      uuid.UUID
	UserSessionIssuerID uuid.UUID
	MCPSlug             string
	ConnectionID        uuid.UUID
	Generation          uuid.UUID
	HandoffID           uuid.UUID
}

// ProviderSetupResult carries only transient provider setup state. It must not
// include a bearer token, provider secret, or readiness fingerprint.
type ProviderSetupResult struct {
	AuthorizationURL string
}

type ProviderReadinessProbeRequest struct {
	UserID              string
	OrganizationID      string
	ProjectID           uuid.UUID
	RegistrationID      uuid.UUID
	UserSessionIssuerID uuid.UUID
	ConnectionID        uuid.UUID
	Generation          uuid.UUID
}

// ProviderReadinessProbeResult normalizes the result of authenticated MCP
// initialize and tools/list negotiation. Adapters return durable authorization
// identity, never an opaque caller-defined fingerprint, raw protocol bodies,
// remote URLs, headers, credentials, or tokens.
type ProviderReadinessProbeResult struct {
	AuthorizationIdentity ProviderAuthorizationIdentity
	State                 ReadinessState
	EvidenceCode          string
	CheckedAt             time.Time
	ExpiresAt             time.Time
}

// ProviderAdapter is implemented only for a reviewed provider or the local
// deterministic fixture. The Platform MCP never accepts arbitrary adapters or
// provider endpoints from an MCP caller.
type ProviderAdapter interface {
	ProviderKey() string
	PreflightSetup(ctx context.Context, request ProviderSetupRequest) error
	BeginSetup(ctx context.Context, request ProviderSetupRequest) (ProviderSetupResult, error)
	ProbeReadiness(ctx context.Context, request ProviderReadinessProbeRequest) (ProviderReadinessProbeResult, error)
}

type ProviderAdapters struct {
	byProviderKey map[string]ProviderAdapter
}

func NewProviderAdapters(adapters []ProviderAdapter) *ProviderAdapters {
	byProviderKey := make(map[string]ProviderAdapter, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil || adapter.ProviderKey() == "" {
			continue
		}
		byProviderKey[adapter.ProviderKey()] = adapter
	}
	return &ProviderAdapters{byProviderKey: byProviderKey}
}

func (a *ProviderAdapters) Get(providerKey string) (ProviderAdapter, error) {
	if a == nil || providerKey == "" {
		return nil, ErrProviderAdapterUnavailable
	}
	adapter, ok := a.byProviderKey[providerKey]
	if !ok {
		return nil, ErrProviderAdapterUnavailable
	}
	return adapter, nil
}

func validateProviderSetupRequest(request ProviderSetupRequest) error {
	if request.UserID == "" || request.OrganizationID == "" || request.ProjectID == uuid.Nil || request.RegistrationID == uuid.Nil || request.UserSessionIssuerID == uuid.Nil || request.MCPSlug == "" || request.ConnectionID == uuid.Nil || request.Generation == uuid.Nil || request.HandoffID == uuid.Nil {
		return ErrSetupHandoffInvalid
	}
	return nil
}

func validateProviderSetupPreflightRequest(request ProviderSetupRequest) error {
	if request.UserID == "" || request.OrganizationID == "" || request.ProjectID == uuid.Nil || request.RegistrationID == uuid.Nil || request.UserSessionIssuerID == uuid.Nil || request.MCPSlug == "" || request.ConnectionID == uuid.Nil || request.Generation == uuid.Nil || request.HandoffID != uuid.Nil {
		return ErrSetupHandoffInvalid
	}
	return nil
}

func validateProviderSetupResult(result ProviderSetupResult) error {
	parsed, err := url.Parse(result.AuthorizationURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrSetupHandoffInvalid
	}
	return nil
}

func validateProviderReadinessProbeRequest(request ProviderReadinessProbeRequest) error {
	if request.UserID == "" || request.OrganizationID == "" || request.ProjectID == uuid.Nil || request.RegistrationID == uuid.Nil || request.UserSessionIssuerID == uuid.Nil || request.ConnectionID == uuid.Nil || request.Generation == uuid.Nil {
		return ErrReadinessInvalid
	}
	return nil
}

func validateProviderReadinessProbeResult(result ProviderReadinessProbeResult) error {
	if _, err := ProviderAuthorizationFingerprint(result.AuthorizationIdentity); err != nil || !isReadinessState(result.State) || result.CheckedAt.IsZero() || result.ExpiresAt.IsZero() || !result.ExpiresAt.After(result.CheckedAt) {
		return ErrReadinessInvalid
	}
	return nil
}
