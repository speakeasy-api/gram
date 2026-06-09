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

const (
	ProviderCursor              = "cursor"
	ProviderAnthropicCompliance = "anthropic_compliance"
)

const (
	initialUsagePollLookback = time.Hour
	usagePollInterval        = time.Hour
	maxUsagePollErrorMessage = 4000
)

type Store struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	enc    *encryption.Client
}

type Config struct {
	ID                     uuid.UUID
	OrganizationID         string
	Provider               string
	ProjectID              uuid.UUID
	ExternalOrganizationID string
	APIKey                 string
	Enabled                bool
	PollWatermarkAt        time.Time
	NextPollAfter          time.Time
	LastPollError          string
	LastPollFailedAt       time.Time
	LastPollSuccessAt      time.Time
	ConsecutiveFailures    int32
	LastCursor             string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type UsagePollCandidate struct {
	ID               uuid.UUID
	OrganizationID   string
	OrganizationSlug string
	Provider         string
}

type UpsertResult struct {
	Config               Config
	Row                  *repo.AiIntegrationConfig
	CreatedNewGeneration bool
}

func NewStore(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client) *Store {
	return &Store{
		logger: logger.With(attr.SlogComponent("aiintegrations")),
		db:     db,
		repo:   repo.New(db),
		enc:    enc,
	}
}

func normalizeProvider(provider string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "", oops.E(oops.CodeInvalid, nil, "provider is required")
	}
	switch provider {
	case ProviderCursor, ProviderAnthropicCompliance:
	default:
		return "", oops.E(oops.CodeInvalid, nil, "unsupported ai integration provider: %s", provider)
	}
	return provider, nil
}

func (s *Store) loadForOrgAndProviderRow(ctx context.Context, orgID string, provider string) (Config, *repo.GetConfigByOrgAndProviderRow, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}

	row, err := s.repo.GetConfigByOrgAndProvider(ctx, repo.GetConfigByOrgAndProviderParams{
		OrganizationID: orgID,
		Provider:       provider,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return emptyConfig(orgID, provider), nil, nil
	case err != nil:
		return emptyConfig(orgID, provider), nil, oops.E(oops.CodeUnexpected, err, "failed to load ai integration config")
	}

	cfg, err := s.configFromGetRow(row)
	if err != nil {
		return emptyConfig(orgID, provider), nil, err
	}
	return cfg, &row, nil
}

func (s *Store) upsertWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, provider string, apiKey string, apiKeySupplied bool, enabled bool, externalOrganizationID string, resetPollWatermarkAt *time.Time) (UpsertResult, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return UpsertResult{}, err
	}
	externalOrganizationID = strings.TrimSpace(externalOrganizationID)
	if provider == ProviderAnthropicCompliance && externalOrganizationID == "" {
		return UpsertResult{}, oops.E(oops.CodeInvalid, nil, "external_organization_id is required for anthropic_compliance")
	}

	q := repo.New(dbtx)
	projectID, err := q.GetFirstProjectByOrganization(ctx, orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UpsertResult{}, oops.E(oops.CodeInvalid, err, "organization has no project for ai integration attribution")
		}
		return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to resolve ai integration project")
	}

	var row repo.AiIntegrationConfig
	createdNewGeneration := apiKeySupplied
	if apiKeySupplied {
		if err := q.SoftDeleteConfig(ctx, repo.SoftDeleteConfigParams{
			OrganizationID: orgID,
			Provider:       provider,
		}); err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to replace ai integration config")
		}
		encrypted, err := s.encryptAPIKey(apiKey)
		if err != nil {
			return UpsertResult{}, err
		}
		row, err = q.InsertConfig(ctx, repo.InsertConfigParams{
			OrganizationID:         orgID,
			Provider:               provider,
			ProjectID:              projectID,
			ExternalOrganizationID: nullableText(externalOrganizationID),
			ApiKeyEncrypted:        encrypted,
			Enabled:                enabled,
		})
		if err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to save ai integration config")
		}
	} else {
		row, err = q.UpdateConfigSettings(ctx, repo.UpdateConfigSettingsParams{
			OrganizationID:         orgID,
			Provider:               provider,
			ProjectID:              projectID,
			ExternalOrganizationID: nullableText(externalOrganizationID),
			Enabled:                enabled,
		})
		if err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to save ai integration config")
		}
	}

	syncRow, err := q.EnsureSync(ctx, row.ID)
	if err != nil {
		return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to save ai integration sync")
	}
	if resetPollWatermarkAt != nil {
		syncRow.PollWatermarkAt = timestamptz(*resetPollWatermarkAt)
		syncRow.NextPollAfter = timestamptz(nextUsagePollAfter(*resetPollWatermarkAt))
		syncRow.LastPollError = pgtype.Text{String: "", Valid: false}
		syncRow.LastPollFailedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		syncRow.LastPollSuccessAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		syncRow.ConsecutiveFailures = 0
		syncRow.LastCursorID = pgtype.Text{String: "", Valid: false}
		if err := q.ResetUsagePollState(ctx, repo.ResetUsagePollStateParams{
			AiIntegrationConfigID: row.ID,
			PollWatermarkAt:       syncRow.PollWatermarkAt,
			NextPollAfter:         syncRow.NextPollAfter,
		}); err != nil {
			return UpsertResult{}, oops.E(oops.CodeUnexpected, err, "failed to reset ai integration sync watermark")
		}
	}

	cfg, err := s.configFromRows(row, syncRow)
	if err != nil {
		return UpsertResult{}, err
	}
	return UpsertResult{
		Config:               cfg,
		Row:                  &row,
		CreatedNewGeneration: createdNewGeneration,
	}, nil
}

func (s *Store) softDeleteWithTx(ctx context.Context, dbtx repo.DBTX, orgID string, provider string) error {
	provider, err := normalizeProvider(provider)
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

func (s *Store) ListEnabledConfigsByProvider(ctx context.Context, provider string) ([]Config, error) {
	provider, err := normalizeProvider(provider)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.ListEnabledConfigsByProvider(ctx, provider)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list ai integration configs")
	}

	configs := make([]Config, 0, len(rows))
	for _, row := range rows {
		cfg, err := s.configFromListRow(row)
		if err != nil {
			s.logger.WarnContext(ctx, "failed to decrypt ai integration config",
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

func (s *Store) ListUsagePollCandidates(ctx context.Context, pollDueBefore time.Time, limit int32) ([]UsagePollCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}

	params := repo.ListUsagePollCandidatesParams{
		PollDueBefore: timestamptz(pollDueBefore),
		LimitCount:    limit,
	}

	rows, err := s.repo.ListUsagePollCandidates(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list ai integration usage poll candidates")
	}

	candidates := make([]UsagePollCandidate, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, UsagePollCandidate{
			ID:               row.ID,
			OrganizationID:   row.OrganizationID,
			OrganizationSlug: row.OrganizationSlug,
			Provider:         row.Provider,
		})
	}
	return candidates, nil
}

func (s *Store) GetUsagePollConfig(ctx context.Context, configID uuid.UUID) (Config, error) {
	row, err := s.repo.GetUsagePollConfigByID(ctx, configID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Config{}, oops.E(oops.CodeNotFound, err, "ai integration usage poll config not found")
	case err != nil:
		return Config{}, oops.E(oops.CodeUnexpected, err, "failed to load ai integration usage poll config")
	}
	cfg, err := s.configFromUsagePollConfigRow(row)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (s *Store) RecordUsagePollSuccess(ctx context.Context, configID uuid.UUID, t time.Time, lastCursor string) error {
	if err := s.repo.RecordUsagePollSuccess(ctx, repo.RecordUsagePollSuccessParams{
		AiIntegrationConfigID: configID,
		PollWatermarkAt:       timestamptz(t),
		NextPollAfter:         timestamptz(nextUsagePollAfter(t)),
		LastCursorID:          nullableText(lastCursor),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record ai integration usage poll success")
	}
	return nil
}

func (s *Store) RecordUsagePollFailure(ctx context.Context, configID uuid.UUID, t time.Time, cause error) error {
	if err := s.repo.RecordUsagePollFailure(ctx, repo.RecordUsagePollFailureParams{
		AiIntegrationConfigID: configID,
		NextPollAfter:         timestamptz(nextUsagePollAfter(t)),
		LastPollError:         nullableText(truncateUsagePollError(cause)),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to record ai integration usage poll failure")
	}
	return nil
}

func initialUsagePollWatermark(now time.Time) time.Time {
	return now.UTC().Add(-initialUsagePollLookback)
}

func nextUsagePollAfter(t time.Time) time.Time {
	return t.UTC().Add(usagePollInterval)
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:             t.UTC(),
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}
}

func nullableText(s string) pgtype.Text {
	return pgtype.Text{
		String: s,
		Valid:  s != "",
	}
}

func truncateUsagePollError(cause error) string {
	if cause == nil {
		return ""
	}
	msg := strings.TrimSpace(cause.Error())
	if len(msg) <= maxUsagePollErrorMessage {
		return msg
	}
	return msg[:maxUsagePollErrorMessage]
}

func (s *Store) configFromGetRow(row repo.GetConfigByOrgAndProviderRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: row.ExternalOrganizationID.String,
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        row.PollWatermarkAt.Time,
		NextPollAfter:          row.NextPollAfter.Time,
		LastPollError:          row.LastPollError.String,
		LastPollFailedAt:       row.LastPollFailedAt.Time,
		LastPollSuccessAt:      row.LastPollSuccessAt.Time,
		ConsecutiveFailures:    row.ConsecutiveFailures,
		LastCursor:             row.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) configFromListRow(row repo.ListEnabledConfigsByProviderRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: row.ExternalOrganizationID.String,
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        row.PollWatermarkAt.Time,
		NextPollAfter:          row.NextPollAfter.Time,
		LastPollError:          row.LastPollError.String,
		LastPollFailedAt:       row.LastPollFailedAt.Time,
		LastPollSuccessAt:      row.LastPollSuccessAt.Time,
		ConsecutiveFailures:    row.ConsecutiveFailures,
		LastCursor:             row.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) configFromUsagePollConfigRow(row repo.GetUsagePollConfigByIDRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: row.ExternalOrganizationID.String,
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        row.PollWatermarkAt.Time,
		NextPollAfter:          row.NextPollAfter.Time,
		LastPollError:          row.LastPollError.String,
		LastPollFailedAt:       row.LastPollFailedAt.Time,
		LastPollSuccessAt:      row.LastPollSuccessAt.Time,
		ConsecutiveFailures:    row.ConsecutiveFailures,
		LastCursor:             row.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) configFromRows(row repo.AiIntegrationConfig, syncRow repo.EnsureSyncRow) (Config, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEncrypted)
	if err != nil {
		return emptyConfig(row.OrganizationID, row.Provider), err
	}
	return Config{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		Provider:               row.Provider,
		ProjectID:              row.ProjectID,
		ExternalOrganizationID: row.ExternalOrganizationID.String,
		APIKey:                 apiKey,
		Enabled:                row.Enabled,
		PollWatermarkAt:        syncRow.PollWatermarkAt.Time,
		NextPollAfter:          syncRow.NextPollAfter.Time,
		LastPollError:          syncRow.LastPollError.String,
		LastPollFailedAt:       syncRow.LastPollFailedAt.Time,
		LastPollSuccessAt:      syncRow.LastPollSuccessAt.Time,
		ConsecutiveFailures:    syncRow.ConsecutiveFailures,
		LastCursor:             syncRow.LastCursorID.String,
		CreatedAt:              row.CreatedAt.Time,
		UpdatedAt:              row.UpdatedAt.Time,
	}, nil
}

func (s *Store) encryptAPIKey(apiKey string) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", oops.E(oops.CodeInvalid, nil, "api_key is required")
	}
	ct, err := s.enc.Encrypt([]byte(apiKey))
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "encrypt ai integration api key")
	}
	return ct, nil
}

func (s *Store) decryptAPIKey(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	plaintext, err := s.enc.Decrypt(stored)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "decrypt ai integration api key")
	}
	return plaintext, nil
}

func emptyConfig(orgID string, provider string) Config {
	return Config{
		ID:                     uuid.Nil,
		OrganizationID:         orgID,
		Provider:               provider,
		ProjectID:              uuid.Nil,
		ExternalOrganizationID: "",
		APIKey:                 "",
		Enabled:                false,
		PollWatermarkAt:        time.Time{},
		NextPollAfter:          time.Time{},
		LastPollError:          "",
		LastPollFailedAt:       time.Time{},
		LastPollSuccessAt:      time.Time{},
		ConsecutiveFailures:    0,
		LastCursor:             "",
		CreatedAt:              time.Time{},
		UpdatedAt:              time.Time{},
	}
}
