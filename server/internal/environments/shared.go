package environments

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/environments/repo"
)

// EnvironmentEntries should be directly accessed through this interface to handle encryption and redaction.
type EnvironmentEntries struct {
	logger *slog.Logger
	repo   *repo.Queries
	enc    *encryption.Encryption
}

func NewEnvironmentEntries(logger *slog.Logger, repo *repo.Queries, enc *encryption.Encryption) *EnvironmentEntries {
	return &EnvironmentEntries{
		logger: logger,
		repo:   repo,
		enc:    enc,
	}
}

func (e *EnvironmentEntries) ListEnvironmentEntries(ctx context.Context, environmentID uuid.UUID, redacted bool) ([]repo.EnvironmentEntry, error) {
	entries, err := e.repo.ListEnvironmentEntries(ctx, environmentID)
	if err != nil {
		return nil, fmt.Errorf("list environment entries: %w", err)
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
