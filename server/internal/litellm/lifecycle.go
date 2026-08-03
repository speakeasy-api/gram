package litellm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	gentypes "github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/litellm/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) CreateInstance(ctx context.Context, payload *gen.CreateInstancePayload) (*gen.LitellmInstanceKeyResult, error) {
	authCtx, projectID, err := s.requireInstanceAdmin(ctx)
	if err != nil {
		return nil, err
	}

	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureAIPlatformPushIntegrations)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check AI platform push integrations feature").LogError(ctx, s.logger)
	}
	if !enabled {
		return nil, oops.E(oops.CodeForbidden, nil, "AI platform push integrations are not enabled for this organization")
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name is required")
	}
	failurePosture := payload.FailurePosture
	if failurePosture == "" {
		failurePosture = "fail_closed"
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin LiteLLM instance transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	project, err := projectsrepo.New(dbtx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "project not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "validate LiteLLM project").LogError(ctx, s.logger)
	}

	key, plaintext, err := s.mintInstanceKey(ctx, keysrepo.New(dbtx), authCtx.ActiveOrganizationID, projectID, authCtx.UserID)
	if err != nil {
		return nil, err
	}

	instance, err := queries.CreateLiteLLMInstance(ctx, repo.CreateLiteLLMInstanceParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       projectID,
		ApiKeyID:        key.ID,
		CreatedByUserID: authCtx.UserID,
		Name:            name,
		FailurePosture:  string(failurePosture),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "litellm_instances_project_id_name_key" {
			return nil, oops.E(oops.CodeConflict, err, "a LiteLLM instance named %q already exists in this project", name)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create LiteLLM instance").LogError(ctx, s.logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	projectUUID := uuid.NullUUID{UUID: projectID, Valid: true}
	if err := s.audit.LogKeyCreate(ctx, dbtx, audit.LogKeyCreateEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectUUID, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil, KeyURN: urn.NewAPIKey(key.ID), KeyName: key.Name, Scopes: key.Scopes}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log LiteLLM API key creation").LogError(ctx, s.logger)
	}
	if err := s.audit.LogLiteLLMInstanceCreate(ctx, dbtx, audit.LogLiteLLMInstanceCreateEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil, InstanceURN: urn.NewLiteLLMInstance(instance.ID), InstanceName: instance.Name}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log LiteLLM instance creation").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit LiteLLM instance creation").LogError(ctx, s.logger)
	}

	return &gen.LitellmInstanceKeyResult{Instance: buildInstanceView(instanceView{
		ID: instance.ID, OrganizationID: instance.OrganizationID, ProjectID: project.ID, ProjectName: project.Name, ProjectSlug: project.Slug,
		Name: instance.Name, FailurePosture: instance.FailurePosture, KeyPrefix: key.KeyPrefix, CreatedByUserID: instance.CreatedByUserID,
		CreatedAt: instance.CreatedAt, UpdatedAt: instance.UpdatedAt, LastUsedAt: key.LastAccessedAt, Active: true,
	}), Key: plaintext}, nil
}

func (s *Service) ListInstances(ctx context.Context, _ *gen.ListInstancesPayload) (*gen.ListInstancesResult, error) {
	authCtx, projectID, err := s.requireInstanceAdmin(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := repo.New(s.db).ListLiteLLMInstances(ctx, repo.ListLiteLLMInstancesParams{ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list LiteLLM instances").LogError(ctx, s.logger)
	}
	instances := make([]*gen.LiteLLMInstance, len(rows))
	for i, row := range rows {
		instances[i] = buildInstanceView(instanceView{
			ID: row.ID, OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, ProjectName: row.ProjectName, ProjectSlug: row.ProjectSlug,
			Name: row.Name, FailurePosture: row.FailurePosture, KeyPrefix: row.KeyPrefix, CreatedByUserID: row.CreatedByUserID,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, LastUsedAt: row.LastAccessedAt, Active: row.Active.Bool,
		})
	}
	return &gen.ListInstancesResult{Instances: instances}, nil
}

func (s *Service) RotateInstanceKey(ctx context.Context, payload *gen.RotateInstanceKeyPayload) (*gen.LitellmInstanceKeyResult, error) {
	authCtx, projectID, err := s.requireInstanceAdmin(ctx)
	if err != nil {
		return nil, err
	}
	instanceID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid LiteLLM instance id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin LiteLLM key rotation transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	instance, err := queries.GetLiteLLMInstanceForUpdate(ctx, repo.GetLiteLLMInstanceForUpdateParams{ID: instanceID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "LiteLLM instance not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get LiteLLM instance for key rotation").LogError(ctx, s.logger)
	}
	project, err := projectsrepo.New(dbtx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "validate LiteLLM project").LogError(ctx, s.logger)
	}
	keyQueries := keysrepo.New(dbtx)
	newKey, plaintext, err := s.mintInstanceKey(ctx, keyQueries, authCtx.ActiveOrganizationID, projectID, authCtx.UserID)
	if err != nil {
		return nil, err
	}
	instanceBefore := instance
	oldAPIKeyID := instance.ApiKeyID
	instance, err = queries.RotateLiteLLMInstanceKey(ctx, repo.RotateLiteLLMInstanceKeyParams{NewApiKeyID: newKey.ID, ID: instance.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID, OldApiKeyID: oldAPIKeyID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "rotate LiteLLM instance key").LogError(ctx, s.logger)
	}
	oldKey, err := keyQueries.DeleteAPIKeyByProject(ctx, keysrepo.DeleteAPIKeyByProjectParams{ID: oldAPIKeyID, OrganizationID: authCtx.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke previous LiteLLM API key").LogError(ctx, s.logger)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	projectUUID := uuid.NullUUID{UUID: projectID, Valid: true}
	if err := s.audit.LogKeyCreate(ctx, dbtx, audit.LogKeyCreateEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectUUID, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil, KeyURN: urn.NewAPIKey(newKey.ID), KeyName: newKey.Name, Scopes: newKey.Scopes}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log rotated LiteLLM API key creation").LogError(ctx, s.logger)
	}
	if err := s.audit.LogKeyRevoke(ctx, dbtx, audit.LogKeyRevokeEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectUUID, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil, KeyURN: urn.NewAPIKey(oldKey.ID), KeyName: oldKey.Name, Scopes: oldKey.Scopes}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log previous LiteLLM API key revocation").LogError(ctx, s.logger)
	}
	if err := s.audit.LogLiteLLMInstanceRotateKey(ctx, dbtx, audit.LogLiteLLMInstanceRotateKeyEvent{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil,
		InstanceURN: urn.NewLiteLLMInstance(instance.ID), InstanceName: instance.Name,
		LiteLLMInstanceSnapshotBefore: buildInstanceSnapshot(instanceBefore, oldKey.KeyPrefix, true),
		LiteLLMInstanceSnapshotAfter:  buildInstanceSnapshot(instance, newKey.KeyPrefix, true),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log LiteLLM instance key rotation").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit LiteLLM instance key rotation").LogError(ctx, s.logger)
	}
	return &gen.LitellmInstanceKeyResult{Instance: buildInstanceView(instanceView{
		ID: instance.ID, OrganizationID: instance.OrganizationID, ProjectID: project.ID, ProjectName: project.Name, ProjectSlug: project.Slug,
		Name: instance.Name, FailurePosture: instance.FailurePosture, KeyPrefix: newKey.KeyPrefix, CreatedByUserID: instance.CreatedByUserID,
		CreatedAt: instance.CreatedAt, UpdatedAt: instance.UpdatedAt, LastUsedAt: newKey.LastAccessedAt, Active: true,
	}), Key: plaintext}, nil
}

func (s *Service) RevokeInstance(ctx context.Context, payload *gen.RevokeInstancePayload) error {
	authCtx, projectID, err := s.requireInstanceAdmin(ctx)
	if err != nil {
		return err
	}
	instanceID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid LiteLLM instance id")
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin LiteLLM revocation transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	instance, err := queries.GetLiteLLMInstanceForUpdate(ctx, repo.GetLiteLLMInstanceForUpdateParams{ID: instanceID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get LiteLLM instance for revocation").LogError(ctx, s.logger)
	}
	instance, err = queries.RevokeLiteLLMInstance(ctx, repo.RevokeLiteLLMInstanceParams{ID: instance.ID, ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke LiteLLM instance").LogError(ctx, s.logger)
	}
	oldKey, err := keysrepo.New(dbtx).DeleteAPIKeyByProject(ctx, keysrepo.DeleteAPIKeyByProjectParams{ID: instance.ApiKeyID, OrganizationID: authCtx.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke LiteLLM API key").LogError(ctx, s.logger)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	projectUUID := uuid.NullUUID{UUID: projectID, Valid: true}
	if err := s.audit.LogKeyRevoke(ctx, dbtx, audit.LogKeyRevokeEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectUUID, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil, KeyURN: urn.NewAPIKey(oldKey.ID), KeyName: oldKey.Name, Scopes: oldKey.Scopes}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log LiteLLM API key revocation").LogError(ctx, s.logger)
	}
	if err := s.audit.LogLiteLLMInstanceRevoke(ctx, dbtx, audit.LogLiteLLMInstanceRevokeEvent{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil, InstanceURN: urn.NewLiteLLMInstance(instance.ID), InstanceName: instance.Name}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log LiteLLM instance revocation").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit LiteLLM instance revocation").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) requireInstanceAdmin(ctx context.Context) (*contextvalues.AuthContext, uuid.UUID, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, uuid.Nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, uuid.Nil, err
	}
	return authCtx, *authCtx.ProjectID, nil
}

func (s *Service) mintInstanceKey(ctx context.Context, queries *keysrepo.Queries, organizationID string, projectID uuid.UUID, userID string) (keysrepo.ApiKey, string, error) {
	plaintext, hash, displayPrefix, err := auth.GenerateAPIKeyMaterial(s.keyPrefix)
	if err != nil {
		return keysrepo.ApiKey{}, "", oops.E(oops.CodeUnexpected, err, "generate LiteLLM API key").LogError(ctx, s.logger)
	}
	key, err := queries.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, CreatedByUserID: userID,
		Name:      fmt.Sprintf("%s%d-%s", auth.LiteLLMAPIKeyNamePrefix, time.Now().UTC().UnixMilli(), uuid.NewString()[:8]),
		KeyPrefix: displayPrefix, KeyHash: hash, Scopes: []string{auth.APIKeyScopeHooks.String()},
	})
	if err != nil {
		return keysrepo.ApiKey{}, "", oops.E(oops.CodeUnexpected, err, "create LiteLLM API key").LogError(ctx, s.logger)
	}
	return key, plaintext, nil
}

type instanceView struct {
	ID              uuid.UUID
	OrganizationID  string
	ProjectID       uuid.UUID
	ProjectName     string
	ProjectSlug     string
	Name            string
	FailurePosture  string
	KeyPrefix       string
	CreatedByUserID string
	CreatedAt       pgtype.Timestamptz
	UpdatedAt       pgtype.Timestamptz
	LastUsedAt      pgtype.Timestamptz
	Active          bool
}

func buildInstanceView(instance instanceView) *gen.LiteLLMInstance {
	return &gen.LiteLLMInstance{
		ID: instance.ID.String(), OrganizationID: instance.OrganizationID,
		Project: &gen.ProjectEntry{ID: instance.ProjectID.String(), Name: instance.ProjectName, Slug: gentypes.Slug(instance.ProjectSlug)},
		Name:    instance.Name, FailurePosture: gen.LiteLLMFailurePosture(instance.FailurePosture), KeyPrefix: instance.KeyPrefix,
		CreatedByUserID: instance.CreatedByUserID, CreatedAt: instance.CreatedAt.Time.Format(time.RFC3339), UpdatedAt: instance.UpdatedAt.Time.Format(time.RFC3339),
		LastUsedAt: formattedTimestamp(instance.LastUsedAt), Active: instance.Active,
	}
}

func buildInstanceSnapshot(instance repo.LitellmInstance, keyPrefix string, active bool) *audit.LiteLLMInstanceSnapshot {
	return &audit.LiteLLMInstanceSnapshot{
		Name: instance.Name, ProjectID: instance.ProjectID, FailurePosture: instance.FailurePosture, KeyPrefix: keyPrefix, Active: active,
	}
}

func formattedTimestamp(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	formatted := value.Time.Format(time.RFC3339)
	return &formatted
}
