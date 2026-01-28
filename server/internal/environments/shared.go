package environments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
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
		logger:          logger,
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
		case errors.Is(err, sql.ErrNoRows):
			return nil, toolconfig.ErrNotFound
		case err != nil:
			return nil, fmt.Errorf("get environment by slug: %w", err)
		}

		environmentID = envModel.ID
	}

	if environmentID == uuid.Nil {
		return nil, fmt.Errorf("environment not found for slug or id: %s", envIDOrSlug)
	}

	entries, err := e.ListEnvironmentEntries(ctx, projectID, environmentID, false)
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
		if errors.Is(err, sql.ErrNoRows) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("get environment for source: %w", err)
	}

	entries, err := e.ListEnvironmentEntries(ctx, projectID, sourceEnv.ID, false)
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
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get environment for toolset: %w", err)
	}

	entries, err := e.ListEnvironmentEntries(ctx, projectID, toolsetEnv.ID, false)
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
	mcpMetadata, err := e.mcpMetadataRepo.GetMetadataForToolset(ctx, toolsetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
	entries, err := e.ListEnvironmentEntries(ctx, projectID, mcpMetadata.DefaultEnvironmentID.UUID, false)
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

func (e *EnvironmentEntries) ListEnvironmentEntries(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID, redacted bool) ([]repo.EnvironmentEntry, error) {
	entries, err := e.repo.ListEnvironmentEntries(ctx, repo.ListEnvironmentEntriesParams{
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	decryptedEntries := make([]repo.EnvironmentEntry, len(entries))
	for i, entry := range entries {
		value, err := e.enc.Decrypt(entry.Value)
		if err != nil {
			return nil, fmt.Errorf("decrypt entry %s: %w", entry.Name, err)
		}

		if redacted {
			value = redactedEnvironment(value)
		}

		decryptedEntries[i] = repo.EnvironmentEntry{
			Name:          entry.Name,
			Value:         value,
			EnvironmentID: entry.EnvironmentID,
			CreatedAt:     entry.CreatedAt,
			UpdatedAt:     entry.UpdatedAt,
		}
	}

	return decryptedEntries, nil
}

func (e *EnvironmentEntries) CreateEnvironmentEntries(ctx context.Context, params repo.CreateEnvironmentEntriesParams) ([]repo.EnvironmentEntry, error) {
	encryptedValues := make([]string, len(params.Values))
	originalValues := make(map[string]string, len(params.Values))

	for i, value := range params.Values {
		encryptedValue, err := e.enc.Encrypt([]byte(value))
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt value for entry %s: %w", params.Names[i], err)
		}
		encryptedValues[i] = encryptedValue
		originalValues[params.Names[i]] = value // avoid having to needlessly decrypt the value
	}

	params.Values = encryptedValues
	createdEntries, err := e.repo.CreateEnvironmentEntries(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create environment entries: %w", err)
	}

	decryptedEntries := make([]repo.EnvironmentEntry, len(createdEntries))
	for i, entry := range createdEntries {
		decryptedEntries[i] = repo.EnvironmentEntry{
			Name:          entry.Name,
			Value:         redactedEnvironment(originalValues[entry.Name]), // avoid having to needlessly decrypt the value
			EnvironmentID: entry.EnvironmentID,
			CreatedAt:     entry.CreatedAt,
			UpdatedAt:     entry.UpdatedAt,
		}
	}

	return decryptedEntries, nil
}

func (e *EnvironmentEntries) UpdateEnvironmentEntry(ctx context.Context, params repo.UpsertEnvironmentEntryParams) error {
	encryptedValue, err := e.enc.Encrypt([]byte(params.Value))
	if err != nil {
		return fmt.Errorf("failed to encrypt value: %w", err)
	}

	params.Value = encryptedValue
	_, err = e.repo.UpsertEnvironmentEntry(ctx, params)
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

func redactedEnvironment(val string) string {
	if val == "" {
		return "<EMPTY>"
	}
	if len(val) <= 3 {
		return strings.Repeat("*", 5)
	}
	return val[:3] + strings.Repeat("*", 5)
}
