package aiintegrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/ai_integrations"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/anthropicinference"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const ProviderAnthropicInference = "anthropic_inference"

// inferenceSecrets is encrypted as a unit; neither secret is exposed in API responses or audit snapshots.
type inferenceSecrets struct {
	Current           string    `json:"current"`
	Previous          string    `json:"previous"`
	PreviousExpiresAt time.Time `json:"previous_expires_at"`
}

// AnthropicInferenceResolver loads the current organization binding and signing keys for each delivery.
type AnthropicInferenceResolver struct {
	db  *pgxpool.Pool
	enc *encryption.Client
}

func NewAnthropicInferenceResolver(db *pgxpool.Pool, enc *encryption.Client) *AnthropicInferenceResolver {
	return &AnthropicInferenceResolver{db: db, enc: enc}
}

func (r *AnthropicInferenceResolver) Resolve(ctx context.Context, id string) (anthropicinference.Config, error) {
	var empty anthropicinference.Config
	configID, err := uuid.Parse(id)
	if err != nil {
		return empty, fmt.Errorf("invalid integration identifier: %w", err)
	}
	row, err := repo.New(r.db).GetAnthropicInferenceConfigByID(ctx, configID)
	if err != nil {
		return empty, fmt.Errorf("load inference integration: %w", err)
	}
	secrets, err := decryptInferenceSecrets(r.enc, row.ApiKeyEncrypted)
	if err != nil {
		return empty, err
	}
	if !row.Enabled && secrets.Current != "" {
		return empty, errors.New("integration disabled")
	}
	keys := []string(nil)
	if secrets.Current != "" {
		keys = append(keys, secrets.Current)
	}
	if secrets.Previous != "" && time.Now().Before(secrets.PreviousExpiresAt) {
		keys = append(keys, secrets.Previous)
	}
	return anthropicinference.Config{ID: row.ID.String(), OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, TenantID: "", SigningSecrets: keys}, nil
}

func decryptInferenceSecrets(enc *encryption.Client, ciphertext string) (inferenceSecrets, error) {
	var secrets inferenceSecrets
	// An empty value represents a pending setup with no credentials, including demo fixtures.
	if ciphertext == "" {
		return secrets, nil
	}
	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		return secrets, fmt.Errorf("decrypt inference credentials: %w", err)
	}
	if err := json.Unmarshal([]byte(plaintext), &secrets); err != nil {
		return secrets, errors.New("invalid inference credentials")
	}
	return secrets, nil
}

func inferenceView(row repo.AiIntegrationConfig, secrets inferenceSecrets) *gen.AnthropicInferenceConfig {
	return &gen.AnthropicInferenceConfig{ID: conv.PtrEmpty(row.ID.String()), WebhookPath: conv.PtrEmpty("/hooks/anthropic-inference/" + row.ID.String()), HasSigningSecret: secrets.Current != "", Enabled: row.Enabled}
}

func (s *Service) GetAnthropicInferenceConfig(ctx context.Context, _ *gen.GetAnthropicInferenceConfigPayload) (*gen.AnthropicInferenceConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	row, err := s.store.repo.GetAnthropicInferenceConfig(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &gen.AnthropicInferenceConfig{ID: nil, WebhookPath: nil, HasSigningSecret: false, Enabled: false}, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load inference integration")
	}
	secrets, err := decryptInferenceSecrets(s.store.enc, row.ApiKeyEncrypted)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read inference credentials")
	}
	return inferenceView(row, secrets), nil
}

func (s *Service) UpsertAnthropicInferenceConfig(ctx context.Context, payload *gen.UpsertAnthropicInferenceConfigPayload) (*gen.AnthropicInferenceConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if payload.SigningSecret != nil {
		if _, err := anthropicinference.DecodeSigningSecret(strings.TrimSpace(*payload.SigningSecret)); err != nil {
			return nil, oops.E(oops.CodeInvalid, nil, "Enter a valid Anthropic signing secret")
		}
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin inference setup")
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.LockAnthropicInferenceConfig(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock inference setup")
	}
	before, err := queries.GetAnthropicInferenceConfig(ctx, authCtx.ActiveOrganizationID)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "load inference setup")
	}
	var secrets inferenceSecrets
	var beforeSnapshot *audit.AIIntegrationSnapshot
	projectID := before.ProjectID
	enabled := before.Enabled
	if exists {
		secrets, err = decryptInferenceSecrets(s.store.enc, before.ApiKeyEncrypted)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read inference credentials")
		}
		beforeSnapshot = &audit.AIIntegrationSnapshot{Provider: ProviderAnthropicInference, ProjectID: projectID, Enabled: enabled, HasAPIKey: secrets.Current != "", BillingMode: ""}
	} else {
		projectID, err = queries.GetFirstProjectByOrganization(ctx, authCtx.ActiveOrganizationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeInvalid, nil, "Create a project before connecting Anthropic inference hooks")
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "assign inference project")
		}
	}
	if payload.SigningSecret != nil && strings.TrimSpace(*payload.SigningSecret) != secrets.Current {
		secrets.Previous = secrets.Current
		secrets.PreviousExpiresAt = time.Now().Add(5 * time.Minute)
		secrets.Current = strings.TrimSpace(*payload.SigningSecret)
	}
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	if enabled && secrets.Current == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "Add the Anthropic signing secret before enabling inference hooks")
	}
	plaintext, err := json.Marshal(secrets)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode inference credentials")
	}
	ciphertext, err := s.store.enc.Encrypt(plaintext)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encrypt inference credentials")
	}
	var row repo.AiIntegrationConfig
	if exists {
		row, err = queries.UpdateAnthropicInferenceConfig(ctx, repo.UpdateAnthropicInferenceConfigParams{ID: before.ID, OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, ApiKeyEncrypted: ciphertext, Enabled: enabled})
	} else {
		row, err = queries.InsertConfig(ctx, repo.InsertConfigParams{OrganizationID: authCtx.ActiveOrganizationID, Provider: ProviderAnthropicInference, ProjectID: projectID, ExternalOrganizationID: pgtype.Text{String: "", Valid: false}, ApiKeyEncrypted: ciphertext, Enabled: enabled, BillingMode: pgtype.Text{String: "", Valid: false}})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save inference integration")
	}
	if err := s.audit.LogAIIntegrationUpsert(ctx, dbtx, audit.LogAIIntegrationUpsertEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil, ConfigURN: urn.NewAIIntegrationConfig(row.ID), SnapshotBefore: beforeSnapshot, SnapshotAfter: &audit.AIIntegrationSnapshot{Provider: ProviderAnthropicInference, ProjectID: projectID, Enabled: enabled, HasAPIKey: secrets.Current != "", BillingMode: ""}}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit inference setup")
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit inference setup")
	}
	return inferenceView(row, secrets), nil
}

func (s *Service) DeleteAnthropicInferenceConfig(ctx context.Context, _ *gen.DeleteAnthropicInferenceConfigPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin inference disconnect")
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.LockAnthropicInferenceConfig(ctx, authCtx.ActiveOrganizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock inference setup")
	}
	before, err := queries.GetAnthropicInferenceConfig(ctx, authCtx.ActiveOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load inference integration")
	}
	row, err := queries.DeleteAnthropicInferenceConfig(ctx, repo.DeleteAnthropicInferenceConfigParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: before.ProjectID})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "disconnect inference integration")
	}
	if err := s.audit.LogAIIntegrationDelete(ctx, dbtx, audit.LogAIIntegrationDeleteEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: row.ProjectID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), ActorDisplayName: authCtx.Email, ActorSlug: nil, ConfigURN: urn.NewAIIntegrationConfig(row.ID)}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit inference disconnect")
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit inference disconnect")
	}
	return nil
}
