package modelkeys

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// ProviderOpenRouter is the only supported key provider: BYOK egress stays
	// on OpenRouter (passthrough), so customer keys are OpenRouter keys.
	ProviderOpenRouter = "openrouter"

	// SlotDefault is the project-wide key slot. It applies to every
	// responsibility slot that has no dedicated override row.
	SlotDefault = "default"
)

// ValidSlots lists every value the slot column accepts: the project-wide
// default plus each completion surface that resolves a key per request.
func ValidSlots() []string {
	slots := []string{SlotDefault}
	for _, source := range billing.ModelUsageSources() {
		// Risk-policy analysis is platform-initiated inference; its BYOK slot
		// ships with the dedicated risk/PI phase, together with lifting the
		// KeyTypeInternal short-circuit in ResolveKey.
		if source == billing.ModelUsageSourceRiskAnalysis {
			continue
		}
		slots = append(slots, string(source))
	}
	// Assistants is deliberately not a registered (billed) usage source while
	// Speakeasy covers its inference, but it is a valid BYOK slot.
	return append(slots, string(billing.ModelUsageSourceAssistants))
}

// Resolver picks the OpenRouter API key a completion bills to. Precedence:
// slot-override key, then the project's default-slot key, then the org's
// provisioned platform key (delegated to the Provisioner, unchanged).
//
// The custom-model-keys product feature gates configuration (the write path)
// only: keys already stored keep resolving if the feature is later disabled.
// Disabling resolution requires disabling or deleting the keys themselves.
type Resolver struct {
	db       *pgxpool.Pool
	enc      *encryption.Client
	platform openrouter.PlatformKeyResolver
}

var _ openrouter.KeyResolver = (*Resolver)(nil)

func NewResolver(db *pgxpool.Pool, enc *encryption.Client, provisioner openrouter.Provisioner) *Resolver {
	return &Resolver{
		db:       db,
		enc:      enc,
		platform: openrouter.PlatformKeyResolver{Provisioner: provisioner},
	}
}

func (r *Resolver) ResolveKey(ctx context.Context, orgID string, projectID string, slot billing.ModelUsageSource, keyType openrouter.KeyType) (openrouter.ResolvedKey, error) {
	// Callers without a project (e.g. embeddings) resolve straight to the
	// platform key. So does KeyTypeInternal: it marks platform-initiated
	// inference (risk judge, prompt-injection scanner), which stays on the
	// platform's internal key until the dedicated risk/PI BYOK slots ship —
	// a project default key must not capture it.
	if projectID == "" || keyType.OrDefault() == openrouter.KeyTypeInternal {
		resolved, err := r.platform.ResolveKey(ctx, orgID, projectID, slot, keyType)
		if err != nil {
			return openrouter.ResolvedKey{}, fmt.Errorf("resolve platform key: %w", err)
		}
		return resolved, nil
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return openrouter.ResolvedKey{}, fmt.Errorf("invalid project id for key resolution: %w", err)
	}

	encryptedKey, err := repo.New(r.db).GetKeyForResolution(ctx, repo.GetKeyForResolutionParams{
		ProjectID:     projectUUID,
		Slots:         []string{string(slot), SlotDefault},
		PreferredSlot: string(slot),
		Provider:      ProviderOpenRouter,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		resolved, err := r.platform.ResolveKey(ctx, orgID, projectID, slot, keyType)
		if err != nil {
			return openrouter.ResolvedKey{}, fmt.Errorf("resolve platform key: %w", err)
		}
		return resolved, nil
	case err != nil:
		return openrouter.ResolvedKey{}, fmt.Errorf("read model provider keys: %w", err)
	}

	key, err := r.enc.Decrypt(encryptedKey)
	if err != nil {
		return openrouter.ResolvedKey{}, fmt.Errorf("decrypt model provider key: %w", err)
	}
	return openrouter.ResolvedKey{Key: key, Customer: true}, nil
}
