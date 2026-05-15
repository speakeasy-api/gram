package cursorintegration

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cursorintegration/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Client struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	enc    *encryption.Client
}

type Config struct {
	ID             uuid.UUID
	OrganizationID string
	ProjectID      uuid.UUID
	APIKey         string
	Enabled        bool
	LastPolledAt   time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewClient(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client) *Client {
	return &Client{
		logger: logger.With(attr.SlogComponent("cursorintegration")),
		db:     db,
		repo:   repo.New(db),
		enc:    enc,
	}
}

func (c *Client) LoadForProjectRow(ctx context.Context, orgID string, projectID uuid.UUID) (Config, *repo.CursorIntegrationConfig, error) {
	row, err := c.repo.GetConfigByProject(ctx, repo.GetConfigByProjectParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return emptyConfig(orgID, projectID), nil, nil
	case err != nil:
		return emptyConfig(orgID, projectID), nil, oops.E(oops.CodeUnexpected, err, "failed to load cursor integration config")
	}

	cfg, err := c.rowToConfig(row)
	if err != nil {
		return emptyConfig(orgID, projectID), nil, err
	}
	return cfg, &row, nil
}

func (c *Client) UpsertWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, projectID uuid.UUID, apiKey string, enabled bool) (Config, *repo.CursorIntegrationConfig, error) {
	encrypted, err := c.encryptAPIKey(apiKey)
	if err != nil {
		return emptyConfig(orgID, projectID), nil, err
	}

	row, err := repo.New(dbtx).UpsertConfig(ctx, repo.UpsertConfigParams{
		OrganizationID:  orgID,
		ProjectID:       projectID,
		ApiKeyEncrypted: encrypted,
		Enabled:         enabled,
	})
	if err != nil {
		return emptyConfig(orgID, projectID), nil, oops.E(oops.CodeUnexpected, err, "failed to save cursor integration config")
	}

	cfg, err := c.rowToConfig(row)
	if err != nil {
		return emptyConfig(orgID, projectID), nil, err
	}
	return cfg, &row, nil
}

func (c *Client) SoftDeleteWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, projectID uuid.UUID) error {
	if err := repo.New(dbtx).SoftDeleteConfig(ctx, repo.SoftDeleteConfigParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete cursor integration config")
	}
	return nil
}

func (c *Client) ListEnabledConfigs(ctx context.Context) ([]Config, error) {
	rows, err := c.repo.ListEnabledConfigs(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list cursor integration configs")
	}

	configs := make([]Config, 0, len(rows))
	for _, row := range rows {
		cfg, err := c.rowToConfig(row)
		if err != nil {
			c.logger.WarnContext(ctx, "failed to decrypt cursor integration config",
				attr.SlogError(err),
				attr.SlogOrganizationID(row.OrganizationID),
				attr.SlogProjectID(row.ProjectID.String()),
			)
			continue
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

func (c *Client) UpdateLastPolledAt(ctx context.Context, id uuid.UUID, t time.Time) error {
	if err := c.repo.UpdateLastPolledAt(ctx, repo.UpdateLastPolledAtParams{
		ID: id,
		LastPolledAt: pgtype.Timestamptz{
			Time:  t.UTC(),
			Valid: true,
		},
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to update cursor poll watermark")
	}
	return nil
}

func (c *Client) rowToConfig(row repo.CursorIntegrationConfig) (Config, error) {
	apiKey, err := c.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.ProjectID), err
	}
	return Config{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID,
		APIKey:         apiKey,
		Enabled:        row.Enabled,
		LastPolledAt:   row.LastPolledAt.Time,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}, nil
}

func (c *Client) encryptAPIKey(apiKey string) (pgtype.Text, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return pgtype.Text{}, oops.E(oops.CodeInvalid, nil, "api_key is required")
	}
	ct, err := c.enc.Encrypt([]byte(apiKey))
	if err != nil {
		return pgtype.Text{}, oops.E(oops.CodeUnexpected, err, "encrypt cursor api key")
	}
	return pgtype.Text{String: ct, Valid: true}, nil
}

func (c *Client) decryptAPIKey(stored pgtype.Text) (string, error) {
	if !stored.Valid || stored.String == "" {
		return "", nil
	}
	plaintext, err := c.enc.Decrypt(stored.String)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "decrypt cursor api key")
	}
	return plaintext, nil
}

func emptyConfig(orgID string, projectID uuid.UUID) Config {
	return Config{
		ID:             uuid.Nil,
		OrganizationID: orgID,
		ProjectID:      projectID,
		APIKey:         "",
		Enabled:        false,
		LastPolledAt:   time.Time{},
		CreatedAt:      time.Time{},
		UpdatedAt:      time.Time{},
	}
}
