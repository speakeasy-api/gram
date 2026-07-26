package modelkeys

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	ProviderOpenRouter = string(openrouter.KeyProviderOpenRouter)
	ProviderAnthropic  = string(openrouter.KeyProviderAnthropic)

	// SlotDefault is the project-wide key slot. It applies to every
	// responsibility slot that has no dedicated override row.
	SlotDefault = "default"
)

// ValidSlots lists every value the slot column accepts: the project-wide
// default plus each completion surface that resolves a key per request.
func ValidSlots() []string {
	slots := []string{SlotDefault}
	for _, source := range billing.ModelUsageSources() {
		// Risk-analysis completions resolve through the dedicated judge slots
		// below, never through a slot named after their shared usage source.
		if source == billing.ModelUsageSourceRiskAnalysis {
			continue
		}
		slots = append(slots, string(source))
	}
	// Assistants is deliberately not a registered (billed) usage source while
	// Speakeasy covers its inference, but it is a valid BYOK slot; so is each
	// platform-initiated judge slot.
	slots = append(slots, string(billing.ModelUsageSourceAssistants))
	for _, slot := range internalBYOKSlots {
		slots = append(slots, string(slot))
	}
	return slots
}

// Resolver picks the provider API key a completion bills to. Precedence:
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
var _ openrouter.ModelAwareKeyResolver = (*Resolver)(nil)

func NewResolver(db *pgxpool.Pool, enc *encryption.Client, provisioner openrouter.Provisioner) *Resolver {
	return &Resolver{
		db:       db,
		enc:      enc,
		platform: openrouter.PlatformKeyResolver{Provisioner: provisioner},
	}
}

func (r *Resolver) ResolveKey(ctx context.Context, orgID string, projectID string, slot billing.ModelUsageSource, keyType openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return r.ResolveKeyForModel(ctx, orgID, projectID, slot, keyType, "")
}

func (r *Resolver) ResolveKeyForModel(ctx context.Context, orgID string, projectID string, slot billing.ModelUsageSource, keyType openrouter.KeyType, model string) (openrouter.ResolvedKey, error) {
	// Callers without a project (e.g. embeddings) resolve straight to the
	// platform key. KeyTypeInternal marks platform-initiated inference; only
	// the slots exposed for it (the risk-analysis judges) consult customer
	// keys. Every other internal completion (chat titles, segment analysis,
	// memory contradiction, naming helpers) stays on the platform's internal
	// key — a project default key must not capture it.
	if projectID == "" || (keyType.OrDefault() == openrouter.KeyTypeInternal && !internalSlotWithBYOK(slot)) {
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

	providers := []string{ProviderOpenRouter}
	if openrouter.KeyProviderAnthropic.SupportsModel(model) {
		providers = append(providers, ProviderAnthropic)
	}

	storedKey, err := repo.New(r.db).GetKeyForResolution(ctx, repo.GetKeyForResolutionParams{
		ProjectID:     projectUUID,
		Slots:         []string{string(slot), SlotDefault},
		Providers:     providers,
		PreferredSlot: string(slot),
	})
	switch {
	// An absent table means BYOK is not deployed in this environment yet
	// (rolling deploy ahead of the migration, or a rollback that kept the
	// code). No key can be configured then, so the platform key is correct —
	// completions must not fail on it.
	case errors.Is(err, pgx.ErrNoRows), isUndefinedTable(err):
		resolved, err := r.platform.ResolveKey(ctx, orgID, projectID, slot, keyType)
		if err != nil {
			return openrouter.ResolvedKey{}, fmt.Errorf("resolve platform key: %w", err)
		}
		return resolved, nil
	case err != nil:
		return openrouter.ResolvedKey{}, fmt.Errorf("read model provider keys: %w", err)
	}

	provider := openrouter.KeyProvider(storedKey.Provider)
	key, err := r.enc.Decrypt(storedKey.ApiKeyEncrypted)
	if err != nil {
		return openrouter.ResolvedKey{}, fmt.Errorf("decrypt model provider key: %w", err)
	}
	return openrouter.ResolvedKey{Key: key, Provider: provider, Customer: true}, nil
}

// internalBYOKSlots lists the platform-initiated (KeyTypeInternal) slots a
// project may override with a customer key. Feeding both ValidSlots and the
// ResolveKey gate from this one list keeps them from drifting: a slot
// accepted by UpsertKey but absent from the gate would store keys that are
// silently never consulted.
var internalBYOKSlots = []billing.ModelUsageSource{
	billing.ModelUsageSourceRiskPolicy,
	billing.ModelUsageSourcePromptInjection,
}

func internalSlotWithBYOK(slot billing.ModelUsageSource) bool {
	return slices.Contains(internalBYOKSlots, slot)
}

// InternalOnlySlots returns the slot names whose customer keys only ever pay
// for platform-initiated inference, never chat completions. Consumers that
// need to know whether an organization's chat traffic is customer-funded
// (e.g. credit-exhaustion alerting) must ignore keys in these slots.
func InternalOnlySlots() []string {
	slots := make([]string, len(internalBYOKSlots))
	for i, slot := range internalBYOKSlots {
		slots[i] = string(slot)
	}
	return slots
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UndefinedTable
}
