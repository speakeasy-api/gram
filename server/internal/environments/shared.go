package environments

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/secretmask"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// EnvironmentEntries should be directly accessed through this interface to handle encryption and redaction.
type EnvironmentEntries struct {
	logger          *slog.Logger
	repo            *repo.Queries
	enc             *encryption.Client
	mcpMetadataRepo *mcpmetadata_repo.Queries
}

func NewEnvironmentEntries(logger *slog.Logger, db repo.DBTX, enc *encryption.Client, mcpMetadataRepo *mcpmetadata_repo.Queries) *EnvironmentEntries {
	return &EnvironmentEntries{
		logger:          logger.With(attr.SlogComponent("environment_entries")),
		repo:            repo.New(db),
		enc:             enc,
		mcpMetadataRepo: mcpMetadataRepo,
	}
}

func (e *EnvironmentEntries) Load(ctx context.Context, projectID uuid.UUID, envIDOrSlug toolconfig.SlugOrID) (map[string]string, error) {
	environmentID := envIDOrSlug.ID
	if envIDOrSlug.IsEmpty() {
		return nil, fmt.Errorf("environment id or slug is required")
	}

	if environmentID == uuid.Nil {
		envModel, err := e.repo.GetEnvironmentBySlug(ctx, repo.GetEnvironmentBySlugParams{
			ProjectID: projectID,
			Slug:      strings.ToLower(envIDOrSlug.Slug),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, toolconfig.ErrNotFound
		case err != nil:
			return nil, fmt.Errorf("get environment by slug: %w", err)
		}

		environmentID = envModel.ID
	}

	if environmentID == uuid.Nil {
		return nil, fmt.Errorf("environment not found for slug or id: %s", envIDOrSlug)
	}

	entries, err := e.ListEnvironmentEntries(ctx, projectID, environmentID)
	if err != nil {
		return nil, fmt.Errorf("list environment entries: %w", err)
	}

	envMap := make(map[string]string, len(entries))
	for _, entry := range entries {
		envMap[entry.Name] = entry.Value
	}
	return envMap, nil
}

func (e *EnvironmentEntries) LoadSourceEnv(ctx context.Context, projectID uuid.UUID, sourceKind string, sourceSlug string) (map[string]string, error) {
	sourceEnv, err := e.repo.GetEnvironmentForSource(ctx, repo.GetEnvironmentForSourceParams{
		SourceKind: sourceKind,
		SourceSlug: sourceSlug,
		ProjectID:  projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("get environment for source: %w", err)
	}

	entries, err := e.ListEnvironmentEntries(ctx, projectID, sourceEnv.ID)
	if err != nil {
		return nil, fmt.Errorf("list environment entries: %w", err)
	}

	envMap := make(map[string]string, len(entries))
	for _, entry := range entries {
		envMap[entry.Name] = entry.Value
	}
	return envMap, nil
}

func (e *EnvironmentEntries) LoadToolsetEnv(ctx context.Context, projectID uuid.UUID, toolsetID uuid.UUID) (map[string]string, error) {
	toolsetEnv, err := e.repo.GetEnvironmentForToolset(ctx, repo.GetEnvironmentForToolsetParams{
		ToolsetID: toolsetID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get environment for toolset: %w", err)
	}

	entries, err := e.ListEnvironmentEntries(ctx, projectID, toolsetEnv.ID)
	if err != nil {
		return nil, fmt.Errorf("list environment entries: %w", err)
	}

	envMap := make(map[string]string, len(entries))
	for _, entry := range entries {
		envMap[entry.Name] = entry.Value
	}
	return envMap, nil
}

// LoadMCPAttachedEnvironment loads the environment variables that are attached to the MCP.
// It uses the mcp_environment_entries table to determine which variables to load from the default environment.
func (e *EnvironmentEntries) LoadMCPAttachedEnvironment(
	ctx context.Context,
	projectID uuid.UUID,
	toolsetID uuid.UUID,
) (map[string]string, error) {
	mcpMetadata, err := e.mcpMetadataRepo.GetMetadataByBackendID(ctx, mcpmetadata_repo.GetMetadataByBackendIDParams{ProjectID: projectID, BackendID: toolsetID, ToolsetBackend: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("get metadata for toolset: %w", err)
	}

	// Get the list of variables configured for this MCP
	mcpEnvConfigs, err := e.mcpMetadataRepo.ListEnvironmentConfigs(ctx, mcpMetadata.ID)
	if err != nil {
		return nil, fmt.Errorf("list mcp environment configs: %w", err)
	}

	// If no default environment is set, or no entries are configured, return empty map
	if !mcpMetadata.DefaultEnvironmentID.Valid || len(mcpEnvConfigs) == 0 {
		return map[string]string{}, nil
	}

	// Load the actual environment variable values from the default environment
	entries, err := e.ListEnvironmentEntries(ctx, projectID, mcpMetadata.DefaultEnvironmentID.UUID)
	if err != nil {
		return nil, fmt.Errorf("list environment entries: %w", err)
	}

	// Create a set of variable names that should be included
	includeVars := make(map[string]bool, len(mcpEnvConfigs))
	for _, mcpConfig := range mcpEnvConfigs {
		if mcpConfig.ProvidedBy == "system" {
			includeVars[mcpConfig.VariableName] = true
		}
	}

	// Build the environment map with only the variables configured for this MCP
	envMap := make(map[string]string)
	for _, entry := range entries {
		if includeVars[entry.Name] {
			envMap[entry.Name] = entry.Value
		}
	}

	return envMap, nil
}

// LoadSystemEnv loads and merges source, toolset, and attached environments.
// Merges in order: source env (base) -> toolset env -> attached env (highest priority).
// Returns empty map if no environments exist.
func (e *EnvironmentEntries) LoadSystemEnv(ctx context.Context, projectID uuid.UUID, toolsetID uuid.UUID, sourceKind string, sourceSlug string) (*toolconfig.CaseInsensitiveEnv, error) {
	sourceEnv, err := e.LoadSourceEnv(ctx, projectID, sourceKind, sourceSlug)
	if err != nil {
		return nil, fmt.Errorf("load source environment: %w", err)
	}

	toolsetEnv, err := e.LoadToolsetEnv(ctx, projectID, toolsetID)
	if err != nil {
		return nil, fmt.Errorf("load toolset environment: %w", err)
	}

	attachedEnv, err := e.LoadMCPAttachedEnvironment(ctx, projectID, toolsetID)
	if err != nil {
		return nil, fmt.Errorf("load attached environment: %w", err)
	}

	// Merge: source env (base) + toolset env + attached env (highest priority)
	systemEnv := toolconfig.NewCaseInsensitiveEnv()
	for k, v := range sourceEnv {
		systemEnv.Set(k, v)
	}
	for k, v := range toolsetEnv {
		systemEnv.Set(k, v)
	}
	for k, v := range attachedEnv {
		systemEnv.Set(k, v)
	}

	return systemEnv, nil
}

// RedactedEntry is an environment entry as management reads show it. A secret
// entry keeps only a fixed mask and whether it has a value; there is no field
// for its plaintext, so API responses and audit snapshots built from it cannot
// carry one.
type RedactedEntry struct {
	// name is the environment variable name.
	name string

	// value is the cleartext of a non-secret entry, or a secretmask
	// placeholder for a secret one.
	value string

	// isSecret reports whether the stored value is encrypted and masked in
	// reads.
	isSecret bool

	// hasValue reports whether the stored value is non-empty.
	hasValue bool

	// createdAt is when the entry was created.
	createdAt pgtype.Timestamptz

	// updatedAt is when the entry was last written.
	updatedAt pgtype.Timestamptz
}

// redactEntry builds the management-read form of entry from its plaintext. For
// a secret entry, only whether the plaintext is empty is kept.
func redactEntry(entry repo.EnvironmentEntry, plaintext string) RedactedEntry {
	hasValue := plaintext != ""

	value := plaintext
	if entry.IsSecret {
		value = secretmask.Empty
		if hasValue {
			value = secretmask.Value
		}
	}

	return RedactedEntry{
		name:      entry.Name,
		value:     value,
		isSecret:  entry.IsSecret,
		hasValue:  hasValue,
		createdAt: entry.CreatedAt,
		updatedAt: entry.UpdatedAt,
	}
}

// ListEnvironmentEntries lists an environment's entries with secret values
// decrypted. Use it only where the values are needed, such as tool calls; use
// ListRedactedEnvironmentEntries for anything shown to a caller.
func (e *EnvironmentEntries) ListEnvironmentEntries(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) ([]repo.EnvironmentEntry, error) {
	entries, err := e.repo.ListEnvironmentEntries(ctx, repo.ListEnvironmentEntriesParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return e.decryptEntries(entries)
}

// ListRedactedEnvironmentEntries lists an environment's entries for management
// reads, with every secret value replaced by a fixed mask.
func (e *EnvironmentEntries) ListRedactedEnvironmentEntries(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) ([]RedactedEntry, error) {
	entries, err := e.repo.ListEnvironmentEntries(ctx, repo.ListEnvironmentEntriesParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	redacted := make([]RedactedEntry, len(entries))
	for i, entry := range entries {
		plaintext := entry.Value
		if entry.IsSecret {
			// TODO(AIM-968): empty secrets are stored as ciphertext, so the
			// only way to tell whether one has a value is to decrypt it. The
			// plaintext goes no further than redactEntry. Storing empty
			// secrets as NULL removes this decryption.
			decrypted, err := e.enc.Decrypt(entry.Value)
			if err != nil {
				return nil, fmt.Errorf("decrypt entry %s: %w", entry.Name, err)
			}
			plaintext = decrypted
		}

		redacted[i] = redactEntry(entry, plaintext)
	}

	return redacted, nil
}

// ListEnvironmentEntriesForUpdate is ListEnvironmentEntries with a row lock held
// on every entry for the rest of the transaction. Callers that derive a write
// from what they read — such as preserving a stored value the caller omitted —
// need this so a concurrent write cannot land in between and be reverted.
func (e *EnvironmentEntries) ListEnvironmentEntriesForUpdate(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) ([]repo.EnvironmentEntry, error) {
	entries, err := e.repo.ListEnvironmentEntriesForUpdate(ctx, repo.ListEnvironmentEntriesForUpdateParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return e.decryptEntries(entries)
}

func (e *EnvironmentEntries) decryptEntries(entries []repo.EnvironmentEntry) ([]repo.EnvironmentEntry, error) {
	decryptedEntries := make([]repo.EnvironmentEntry, len(entries))
	for i, entry := range entries {
		value := entry.Value
		if entry.IsSecret {
			decrypted, err := e.enc.Decrypt(entry.Value)
			if err != nil {
				return nil, fmt.Errorf("decrypt entry %s: %w", entry.Name, err)
			}
			value = decrypted
		}

		decryptedEntries[i] = repo.EnvironmentEntry{
			Name:          entry.Name,
			Value:         value,
			IsSecret:      entry.IsSecret,
			EnvironmentID: entry.EnvironmentID,
			CreatedAt:     entry.CreatedAt,
			UpdatedAt:     entry.UpdatedAt,
		}
	}

	return decryptedEntries, nil
}

// CreateEnvironmentEntries stores new entries, encrypting secret values, and
// returns them in their management-read form.
func (e *EnvironmentEntries) CreateEnvironmentEntries(ctx context.Context, params repo.CreateEnvironmentEntriesParams) ([]RedactedEntry, error) {
	storedValues := make([]string, len(params.Values))
	originalValues := make(map[string]string, len(params.Values))

	for i, value := range params.Values {
		storedValue := value
		if params.IsSecrets[i] {
			encryptedValue, err := e.enc.Encrypt([]byte(value))
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt value for entry %s: %w", params.Names[i], err)
			}
			storedValue = encryptedValue
		}
		storedValues[i] = storedValue
		originalValues[params.Names[i]] = value // avoid having to needlessly decrypt the value
	}

	params.Values = storedValues
	createdEntries, err := e.repo.CreateEnvironmentEntries(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create environment entries: %w", err)
	}

	redacted := make([]RedactedEntry, len(createdEntries))
	for i, entry := range createdEntries {
		redacted[i] = redactEntry(entry, originalValues[entry.Name])
	}

	return redacted, nil
}

func (e *EnvironmentEntries) UpdateEnvironmentEntry(ctx context.Context, params repo.UpsertEnvironmentEntryParams) error {
	if params.IsSecret {
		encryptedValue, err := e.enc.Encrypt([]byte(params.Value))
		if err != nil {
			return fmt.Errorf("failed to encrypt value: %w", err)
		}
		params.Value = encryptedValue
	}

	_, err := e.repo.UpsertEnvironmentEntry(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to update environment entry: %w", err)
	}

	return nil
}

func (e *EnvironmentEntries) DeleteEnvironmentEntry(ctx context.Context, params repo.DeleteEnvironmentEntryParams) error {
	err := e.repo.DeleteEnvironmentEntry(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to delete environment entry: %w", err)
	}

	return nil
}
