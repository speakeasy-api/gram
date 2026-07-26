package openrouter

import (
	"context"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/billing"
)

type KeyProvider string

const (
	KeyProviderOpenRouter KeyProvider = "openrouter"
	KeyProviderAnthropic  KeyProvider = "anthropic"
)

func (p KeyProvider) OrDefault() KeyProvider {
	if p == "" {
		return KeyProviderOpenRouter
	}
	return p
}

func (p KeyProvider) SupportsModel(model string) bool {
	switch p.OrDefault() {
	case KeyProviderOpenRouter:
		return true
	case KeyProviderAnthropic:
		return strings.HasPrefix(model, "anthropic/")
	default:
		return false
	}
}

// ResolvedKey is the outcome of key resolution. Customer marks a
// customer-supplied (BYOK) key: platform-side key maintenance — generation
// lookups, limit refresh — only applies to platform-provisioned keys, so
// callers must skip it when the flag is set.
type ResolvedKey struct {
	Key      string
	Provider KeyProvider
	Customer bool
}

// KeyResolver resolves the provider API key a completion should bill to,
// given the requesting org, project, and responsibility slot.
// Implementations fall back to the org's provisioned platform key when no
// customer-supplied key applies.
type KeyResolver interface {
	ResolveKey(ctx context.Context, orgID string, projectID string, slot billing.ModelUsageSource, keyType KeyType) (ResolvedKey, error)
}

// ModelAwareKeyResolver can apply provider compatibility while evaluating a
// slot and its project-default fallback.
type ModelAwareKeyResolver interface {
	ResolveKeyForModel(ctx context.Context, orgID string, projectID string, slot billing.ModelUsageSource, keyType KeyType, model string) (ResolvedKey, error)
}

// PlatformKeyResolver resolves every slot to the org's provisioned platform
// key, ignoring project and slot.
type PlatformKeyResolver struct {
	Provisioner Provisioner
}

var _ KeyResolver = (*PlatformKeyResolver)(nil)

func (r *PlatformKeyResolver) ResolveKey(ctx context.Context, orgID string, _ string, _ billing.ModelUsageSource, keyType KeyType) (ResolvedKey, error) {
	key, err := r.Provisioner.ProvisionAPIKey(ctx, orgID, keyType)
	if err != nil {
		return ResolvedKey{}, fmt.Errorf("provision platform key: %w", err)
	}
	return ResolvedKey{Key: key, Provider: KeyProviderOpenRouter, Customer: false}, nil
}
