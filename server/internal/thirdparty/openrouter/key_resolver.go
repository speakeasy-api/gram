package openrouter

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/billing"
)

// ResolvedKey is the outcome of key resolution. Customer marks a
// customer-supplied (BYOK) key: platform-side key maintenance — generation
// lookups, limit refresh — only applies to platform-provisioned keys, so
// callers must skip it when the flag is set.
type ResolvedKey struct {
	Key      string
	Customer bool
}

// KeyResolver resolves the OpenRouter API key a completion should bill to,
// given the requesting org, project, and responsibility slot (usage source).
// Implementations fall back to the org's provisioned platform key when no
// customer-supplied key applies.
type KeyResolver interface {
	ResolveKey(ctx context.Context, orgID string, projectID string, slot billing.ModelUsageSource, keyType KeyType) (ResolvedKey, error)
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
	return ResolvedKey{Key: key, Customer: false}, nil
}
