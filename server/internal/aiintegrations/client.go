package aiintegrations

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

	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const ProviderCursor = "cursor"

type Client struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	enc    *encryption.Client
}

type Config struct {
	ID             uuid.UUID
	OrganizationID string
	Provider       string
	ProjectID      uuid.UUID
	APIKey         string
	Enabled        bool
	LastPolledAt   time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewClient(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client) *Client {
	return &Client{
		logger: logger.With(attr.SlogComponent("aiintegrations")),
		db:     db,
		repo:   repo.New(db),
		enc:    enc,
	}
}

func NormalizeProvider(provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "", oops.E(oops.CodeInvalid, nil, "provider is required")
	}
	if provider != ProviderCursor {
		return "", oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider: %s", provider)
	}
	return provider, nil
}

func (c *Client) LoadForOrgAndProviderRow(ctx context.Context, orgID string, provider string) (Config, *repo.GetConfigByOrgAndProviderRow, error) {
	provider, err := NormalizeProvider(provider)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}

	row, err := c.repo.GetConfigByOrgAndProvider(ctx, repo.GetConfigByOrgAndProviderParams{
		OrganizationID: orgID,
		Provider:       provider,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return emptyConfig(orgID, provider), nil, nil
	case err != nil:
		return emptyConfig(orgID, provider), nil, oops.E(oops.CodeUnexpected, err, "failed to load ai integration config")
	}

	cfg, err := c.configFromGetRow(row)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}
	return cfg, &row, nil
}

func (c *Client) UpsertWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, provider string, apiKey string, enabled bool) (Config, *repo.AiIntegrationConfig, error) {
	provider, err := NormalizeProvider(provider)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}

	q := repo.New(dbtx)
	projectID, err := q.GetFirstProjectByOrganization(ctx, orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return emptyConfig(orgID, provider), nil, oops.E(oops.CodeInvalid, err, "organization has no project for ai integration attribution")
		}
		return emptyConfig(orgID, provider), nil, oops.E(oops.CodeUnexpected, err, "failed to resolve ai integration project")
	}

	encrypted, err := c.encryptAPIKey(apiKey)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}

	row, err := q.UpsertConfig(ctx, repo.UpsertConfigParams{
		OrganizationID:  orgID,
		Provider:        provider,
		ProjectID:       projectID,
		ApiKeyEncrypted: encrypted,
		Enabled:         enabled,
	})
	if err != nil {
		return emptyConfig(orgID, provider), nil, oops.E(oops.CodeUnexpected, err, "failed to save ai integration config")
	}

	syncRow, err := q.EnsureSync(ctx, row.ID)
	if err != nil {
		return emptyConfig(orgID, provider), nil, oops.E(oops.CodeUnexpected, err, "failed to save ai integration sync")
	}

	cfg, err := c.configFromRows(row, syncRow.LastPolledAt)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}
	return cfg, &row, nil
}

func (c *Client) SoftDeleteWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, provider string) error {
	provider, err := NormalizeProvider(provider)
	if err != nil {
		return err
	}
	if err := repo.New(dbtx).SoftDeleteConfig(ctx, repo.SoftDeleteConfigParams{
		OrganizationID: orgID,
		Provider:       provider,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete ai integration config")
	}
	return nil
}

func (c *Client) ListEnabledConfigsByProvider(ctx context.Context, provider string) ([]Config, error) {
	provider, err := NormalizeProvider(provider)
	if err != nil {
		return nil, err
	}

	rows, err := c.repo.ListEnabledConfigsByProvider(ctx, provider)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list ai integration configs")
	}

	configs := make([]Config, 0, len(rows))
	for _, row := range rows {
		cfg, err := c.configFromListRow(row)
		if err != nil {
			c.logger.WarnContext(ctx, "failed to decrypt ai integration config",
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

func (c *Client) UpdateSyncLastPolledAt(ctx context.Context, configID uuid.UUID, t time.Time) error {
	if err := c.repo.UpdateSyncLastPolledAt(ctx, repo.UpdateSyncLastPolledAtParams{
		AiIntegrationConfigID: configID,
		LastPolledAt: pgtype.Timestamptz{
			Time:  t.UTC(),
			Valid: true,
		},
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to update ai integration sync watermark")
	}
	return nil
}

func (c *Client) configFromGetRow(row repo.GetConfigByOrgAndProviderRow) (Config, error) {
	apiKey, err := c.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		Provider:       row.Provider,
		ProjectID:      row.ProjectID,
		APIKey:         apiKey,
		Enabled:        row.Enabled,
		LastPolledAt:   row.LastPolledAt.Time,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}, nil
}

func (c *Client) configFromListRow(row repo.ListEnabledConfigsByProviderRow) (Config, error) {
	apiKey, err := c.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		Provider:       row.Provider,
		ProjectID:      row.ProjectID,
		APIKey:         apiKey,
		Enabled:        row.Enabled,
		LastPolledAt:   row.LastPolledAt.Time,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}, nil
}

func (c *Client) configFromRows(row repo.AiIntegrationConfig, lastPolledAt pgtype.Timestamptz) (Config, error) {
	apiKey, err := c.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		Provider:       row.Provider,
		ProjectID:      row.ProjectID,
		APIKey:         apiKey,
		Enabled:        row.Enabled,
		LastPolledAt:   lastPolledAt.Time,
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
		return pgtype.Text{}, oops.E(oops.CodeUnexpected, err, "encrypt ai integration api key")
	}
	return pgtype.Text{String: ct, Valid: true}, nil
}

func (c *Client) decryptAPIKey(stored pgtype.Text) (string, error) {
	if !stored.Valid || stored.String == "" {
		return "", nil
	}
	plaintext, err := c.enc.Decrypt(stored.String)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "decrypt ai integration api key")
	}
	return plaintext, nil
}

func emptyConfig(orgID string, provider string) Config {
	return Config{
		ID:             uuid.Nil,
		OrganizationID: orgID,
		Provider:       provider,
		ProjectID:      uuid.Nil,
		APIKey:         "",
		Enabled:        false,
		LastPolledAt:   time.Time{},
		CreatedAt:      time.Time{},
		UpdatedAt:      time.Time{},
	}
}
