package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/skills/server"
	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const maxSkillsRequestBodyBytes = 512 * 1024

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	authz    *authz.Engine
	features *productfeatures.Client
	audit    *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	features *productfeatures.Client,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("skills"))

	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/skills"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		features: features,
		audit:    auditLogger,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, skillsRequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func skillsRequestDecoder(r *http.Request) goahttp.Decoder {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(nil, r.Body, maxSkillsRequestBodyBytes)
	}

	return goahttp.RequestDecoder(r)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) requireAccess(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{
		Scope:        scope,
		ResourceKind: "",
		ResourceID:   authCtx.ProjectID.String(),
		Dimensions:   nil,
	}); err != nil {
		return nil, s.logger, err
	}

	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSkills)
	if err != nil {
		return nil, logger, oops.E(oops.CodeUnexpected, err, "check skills feature").LogError(ctx, logger)
	}
	if !enabled {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "skills are not enabled for this organization")
	}

	return authCtx, logger, nil
}

func manifestErrorMessage(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "content is empty"):
		return "skill manifest content is empty"
	case strings.Contains(message, "content exceeds"), errors.Is(err, errCanonicalDocumentTooLarge):
		return "skill manifest exceeds the 65536 byte limit"
	case strings.Contains(message, "valid UTF-8"):
		return "skill manifest must be valid UTF-8"
	case strings.Contains(message, "contains NUL"):
		return "skill manifest must not contain NUL bytes"
	case strings.Contains(message, "missing opening frontmatter delimiter"):
		return "skill manifest is missing its opening frontmatter delimiter"
	case strings.Contains(message, "missing closing frontmatter delimiter"):
		return "skill manifest is missing its closing frontmatter delimiter"
	case strings.Contains(message, "name is required"):
		return "skill manifest frontmatter requires a name"
	case strings.Contains(message, "name must") || strings.Contains(message, "cannot be normalized"):
		return "skill manifest has an invalid name"
	case strings.Contains(message, "frontmatter"):
		return "skill manifest frontmatter is invalid"
	default:
		return "skill manifest is invalid"
	}
}

func loadDerivedSkillState(ctx context.Context, queries *repo.Queries, projectID, skillID uuid.UUID) (uuid.UUID, int64, error) {
	state, err := queries.GetSkillState(ctx, repo.GetSkillStateParams{
		ProjectID: projectID,
		SkillID:   skillID,
	})
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("get skill state: %w", err)
	}

	return state.LatestVersionID, state.VersionCount, nil
}

func buildSkillAuditSnapshot(skill repo.Skill, latestVersionID uuid.UUID, versionCount int64) *audit.SkillSnapshot {
	var archivedAt *string
	if skill.ArchivedAt.Valid {
		value := conv.FromPGTimestamptz(skill.ArchivedAt)
		archivedAt = &value
	}

	return &audit.SkillSnapshot{
		ID:              skill.ID.String(),
		ProjectID:       skill.ProjectID.String(),
		Name:            skill.Name,
		DisplayName:     skill.DisplayName,
		SourceKind:      skill.SourceKind,
		Classification:  skill.Classification,
		LatestVersionID: latestVersionID.String(),
		VersionCount:    versionCount,
		CreatedAt:       conv.FromPGTimestamptz(skill.CreatedAt),
		UpdatedAt:       conv.FromPGTimestamptz(skill.UpdatedAt),
		ArchivedAt:      archivedAt,
	}
}

func buildSkillDistributionAuditSnapshot(distribution repo.SkillDistribution, resolvedVersionID uuid.UUID) *audit.SkillDistributionSnapshot {
	return &audit.SkillDistributionSnapshot{
		ID:                distribution.ID.String(),
		ProjectID:         distribution.ProjectID.String(),
		SkillID:           distribution.SkillID.String(),
		PluginID:          conv.FromNullableUUID(distribution.PluginID),
		PinnedVersionID:   conv.FromNullableUUID(distribution.PinnedVersionID),
		ResolvedVersionID: resolvedVersionID.String(),
		Channel:           distribution.Channel,
		CreatedByUserID:   distribution.CreatedByUserID,
		RevokedAt:         conv.PtrEmpty(conv.FromPGTimestamptz(distribution.RevokedAt)),
		CreatedAt:         conv.FromPGTimestamptz(distribution.CreatedAt),
		UpdatedAt:         conv.FromPGTimestamptz(distribution.UpdatedAt),
	}
}

func (s *Service) recordVersion(
	ctx context.Context,
	dbtx pgx.Tx,
	queries *repo.Queries,
	authCtx *contextvalues.AuthContext,
	logger *slog.Logger,
	skill repo.Skill,
	parsed parsedSkillManifest,
	createdSkill bool,
) (*gen.RecordSkillResult, error) {
	metadataJSON, err := json.Marshal(parsed.Metadata)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode skill metadata").LogError(ctx, logger)
	}
	validationErrorsJSON, err := json.Marshal(parsed.ValidationErrors)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "encode skill validation errors").LogError(ctx, logger)
	}

	var beforeSnapshot *audit.SkillSnapshot
	if !createdSkill {
		latestBeforeID, countBefore, err := loadDerivedSkillState(ctx, queries, *authCtx.ProjectID, skill.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "load skill state before adding version").LogError(ctx, logger)
		}
		if err == nil {
			beforeSnapshot = buildSkillAuditSnapshot(skill, latestBeforeID, countBefore)
		}
	}

	version, err := queries.CreateSkillVersion(ctx, repo.CreateSkillVersionParams{
		Content:          parsed.RawContent,
		CanonicalSha256:  parsed.CanonicalSHA256,
		RawSha256:        parsed.RawSHA256,
		Description:      conv.PtrToPGText(parsed.Description),
		Metadata:         metadataJSON,
		SpecValid:        parsed.SpecValid,
		ValidationErrors: validationErrorsJSON,
		CreatedByUserID:  authCtx.UserID,
		ProjectID:        *authCtx.ProjectID,
		SkillID:          skill.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		matched, getErr := queries.GetSkillVersionByHash(ctx, repo.GetSkillVersionByHashParams{
			ProjectID:       *authCtx.ProjectID,
			SkillID:         skill.ID,
			CanonicalSha256: parsed.CanonicalSHA256,
		})
		if getErr != nil {
			return nil, oops.E(oops.CodeUnexpected, getErr, "resolve existing skill version after insert no-op").LogError(ctx, logger)
		}
		if deleteErr := queries.DeleteSkillVersionOrigin(ctx, repo.DeleteSkillVersionOriginParams{
			ProjectID:      *authCtx.ProjectID,
			SkillID:        skill.ID,
			SkillVersionID: matched.ID,
		}); deleteErr != nil {
			return nil, oops.E(oops.CodeUnexpected, deleteErr, "promote captured skill version to manual").LogError(ctx, logger)
		}

		latestID, count, stateErr := loadDerivedSkillState(ctx, queries, *authCtx.ProjectID, skill.ID)
		if stateErr != nil {
			return nil, oops.E(oops.CodeUnexpected, stateErr, "load current skill state after version no-op").LogError(ctx, logger)
		}
		matchedView, viewErr := mv.BuildSkillVersionView(matched, manifestFrontmatter(matched.Content))
		if viewErr != nil {
			return nil, oops.E(oops.CodeUnexpected, viewErr, "build existing skill version").LogError(ctx, logger)
		}

		return &gen.RecordSkillResult{
			Skill:          mv.BuildSkillView(skill, latestID, count),
			Version:        matchedView,
			CreatedSkill:   false,
			CreatedVersion: false,
		}, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create skill version").LogError(ctx, logger)
	}

	updated, err := queries.UpdateSkill(ctx, repo.UpdateSkillParams{
		DisplayName: parsed.DisplayName,
		Summary:     conv.PtrToPGText(parsed.Description),
		ProjectID:   *authCtx.ProjectID,
		ID:          skill.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update skill after adding version").LogError(ctx, logger)
	}

	latestID, count, err := loadDerivedSkillState(ctx, queries, *authCtx.ProjectID, skill.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load skill state after adding version").LogError(ctx, logger)
	}
	afterView := mv.BuildSkillView(updated, latestID, count)
	versionView, err := mv.BuildSkillVersionView(version, manifestFrontmatter(version.Content))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build created skill version").LogError(ctx, logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	if createdSkill {
		err = s.audit.LogSkillCreate(ctx, dbtx, audit.LogSkillCreateEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			SkillURN:         urn.NewSkill(updated.ID),
			SkillName:        updated.Name,
			SkillDisplayName: updated.DisplayName,
		})
	} else {
		afterSnapshot := buildSkillAuditSnapshot(updated, latestID, count)
		err = s.audit.LogSkillAddVersion(ctx, dbtx, audit.LogSkillAddVersionEvent{
			OrganizationID:      authCtx.ActiveOrganizationID,
			ProjectID:           *authCtx.ProjectID,
			Actor:               actor,
			ActorDisplayName:    authCtx.Email,
			ActorSlug:           nil,
			SkillURN:            urn.NewSkill(updated.ID),
			SkillName:           updated.Name,
			SkillDisplayName:    updated.DisplayName,
			SkillSnapshotBefore: beforeSnapshot,
			SkillSnapshotAfter:  afterSnapshot,
		})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log skill version mutation").LogError(ctx, logger)
	}

	return &gen.RecordSkillResult{
		Skill:          afterView,
		Version:        versionView,
		CreatedSkill:   createdSkill,
		CreatedVersion: true,
	}, nil
}

func (s *Service) Create(ctx context.Context, payload *gen.CreatePayload) (*gen.RecordSkillResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
		return nil, err
	}

	parsed, err := parseSkillManifest(payload.Content)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s", manifestErrorMessage(err))
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin create skill transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	if err := queries.LockSkillName(ctx, repo.LockSkillNameParams{
		ProjectID: *authCtx.ProjectID,
		Name:      parsed.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill name").LogError(ctx, logger)
	}

	skill, err := queries.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{
		ProjectID: *authCtx.ProjectID,
		Name:      parsed.Name,
	})
	createdSkill := false
	if errors.Is(err, pgx.ErrNoRows) {
		skill, err = queries.CreateSkill(ctx, repo.CreateSkillParams{
			ProjectID:   *authCtx.ProjectID,
			Name:        parsed.Name,
			DisplayName: parsed.DisplayName,
			Summary:     conv.PtrToPGText(parsed.Description),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			skill, err = queries.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{
				ProjectID: *authCtx.ProjectID,
				Name:      parsed.Name,
			})
		} else if err == nil {
			createdSkill = true
		}
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve skill by manifest name").LogError(ctx, logger)
	}
	if !createdSkill && skill.SourceKind == "captured" {
		_, _, stateErr := loadDerivedSkillState(ctx, queries, *authCtx.ProjectID, skill.ID)
		switch {
		case errors.Is(stateErr, pgx.ErrNoRows):
			skill, err = queries.PromoteObservedSkillToManual(ctx, repo.PromoteObservedSkillToManualParams{
				ProjectID: *authCtx.ProjectID,
				ID:        skill.ID,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "promote observed skill to manual").LogError(ctx, logger)
			}
		case stateErr != nil:
			return nil, oops.E(oops.CodeUnexpected, stateErr, "load observed skill state").LogError(ctx, logger)
		}
	}

	result, err := s.recordVersion(ctx, dbtx, queries, authCtx, logger, skill, parsed, createdSkill)
	if err != nil {
		return nil, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit create skill transaction").LogError(ctx, logger)
	}

	return result, nil
}

func (s *Service) AddVersion(ctx context.Context, payload *gen.AddVersionPayload) (*gen.RecordSkillResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
		return nil, err
	}

	skillID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	parsed, err := parseSkillManifest(payload.Content)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "%s", manifestErrorMessage(err))
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin add skill version transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	if err := queries.LockSkillName(ctx, repo.LockSkillNameParams{
		ProjectID: *authCtx.ProjectID,
		Name:      parsed.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill name").LogError(ctx, logger)
	}

	skill, err := queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skillID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "skill not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get skill for version update").LogError(ctx, logger)
	}
	if skill.Name != parsed.Name {
		return nil, oops.E(oops.CodeInvalid, nil, "manifest name does not match the skill")
	}

	result, err := s.recordVersion(ctx, dbtx, queries, authCtx, logger, skill, parsed, false)
	if err != nil {
		return nil, err
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit add skill version transaction").LogError(ctx, logger)
	}

	return result, nil
}

func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.ListSkillsResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillRead)
	if err != nil {
		return nil, err
	}

	cursorName := pgtype.Text{String: "", Valid: false}
	if payload.Cursor != nil {
		name, decodeErr := decodeSkillCursor(*payload.Cursor)
		if decodeErr != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill cursor")
		}
		cursorName = conv.ToPGText(name)
	}

	rows, err := repo.New(s.db).ListSkills(ctx, repo.ListSkillsParams{
		ProjectID:  *authCtx.ProjectID,
		CursorName: cursorName,
		PageLimit:  conv.SafeInt32(payload.Limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list skills").LogError(ctx, logger)
	}

	hasMore := len(rows) > payload.Limit
	if hasMore {
		rows = rows[:payload.Limit]
	}
	var nextCursor *string
	if hasMore {
		encoded := encodeSkillCursor(rows[len(rows)-1].Skill.Name)
		nextCursor = &encoded
	}

	return &gen.ListSkillsResult{
		Skills:     mv.BuildSkillListView(rows),
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) Get(ctx context.Context, payload *gen.GetPayload) (*gen.GetSkillResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillRead)
	if err != nil {
		return nil, err
	}

	skillID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	details, err := repo.New(s.db).GetSkillDetails(ctx, repo.GetSkillDetailsParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   skillID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "skill not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get skill details").LogError(ctx, logger)
	}

	latestView, err := mv.BuildSkillVersionView(details.SkillVersion, manifestFrontmatter(details.SkillVersion.Content))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build latest skill version").LogError(ctx, logger)
	}

	return &gen.GetSkillResult{
		Skill:         mv.BuildSkillView(details.Skill, details.SkillVersion.ID, details.VersionCount),
		LatestVersion: latestView,
	}, nil
}

func (s *Service) ListVersions(ctx context.Context, payload *gen.ListVersionsPayload) (*gen.ListSkillVersionsResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillRead)
	if err != nil {
		return nil, err
	}

	skillID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	cursorCreatedAt := conv.PtrToPGTimestamptz(nil)
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.Cursor != nil {
		createdAt, id, decodeErr := decodeCreatedAtIDCursor(*payload.Cursor)
		if decodeErr != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill version cursor")
		}
		cursorCreatedAt = conv.ToPGTimestamptz(createdAt)
		cursorID = uuid.NullUUID{UUID: id, Valid: true}
	}

	queries := repo.New(s.db)
	rows, err := queries.ListSkillVersions(ctx, repo.ListSkillVersionsParams{
		ProjectID:       *authCtx.ProjectID,
		SkillID:         skillID,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		PageLimit:       conv.SafeInt32(payload.Limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list skill versions").LogError(ctx, logger)
	}
	if len(rows) == 0 {
		if _, err := queries.GetSkill(ctx, repo.GetSkillParams{
			ProjectID: *authCtx.ProjectID,
			ID:        skillID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, nil, "skill not found")
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get skill after empty version page").LogError(ctx, logger)
		}
	}

	hasMore := len(rows) > payload.Limit
	if hasMore {
		rows = rows[:payload.Limit]
	}
	views, err := mv.BuildSkillVersionListView(rows, manifestFrontmatter)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build skill versions").LogError(ctx, logger)
	}
	var nextCursor *string
	if hasMore {
		last := rows[len(rows)-1]
		encoded := encodeCreatedAtIDCursor(last.CreatedAt.Time, last.ID)
		nextCursor = &encoded
	}

	return &gen.ListSkillVersionsResult{
		Versions:   views,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) Distribute(ctx context.Context, payload *gen.DistributePayload) (*types.SkillDistribution, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
		return nil, err
	}

	skillID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid plugin id")
	}
	pinnedVersionID, err := conv.PtrToNullUUID(payload.PinnedVersionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid pinned version id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin distribute skill transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	skill, err := queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{ProjectID: *authCtx.ProjectID, ID: skillID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "skill not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lock skill for distribution").LogError(ctx, logger)
	}

	plugin, err := queries.GetPluginForDistribution(ctx, repo.GetPluginForDistributionParams{
		ProjectID: *authCtx.ProjectID,
		PluginID:  pluginID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeBadRequest, nil, "plugin not found in project")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "validate plugin for distribution").LogError(ctx, logger)
	}

	var resolvedVersionID uuid.UUID
	if pinnedVersionID.Valid {
		version, versionErr := queries.GetValidSkillVersion(ctx, repo.GetValidSkillVersionParams{
			ProjectID: *authCtx.ProjectID,
			SkillID:   skill.ID,
			VersionID: pinnedVersionID.UUID,
		})
		if errors.Is(versionErr, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeBadRequest, nil, "pinned version must be a valid version of the skill")
		}
		if versionErr != nil {
			return nil, oops.E(oops.CodeUnexpected, versionErr, "validate pinned skill version").LogError(ctx, logger)
		}
		resolvedVersionID = version
	} else {
		version, versionErr := queries.GetLatestValidSkillVersion(ctx, repo.GetLatestValidSkillVersionParams{
			ProjectID: *authCtx.ProjectID,
			SkillID:   skill.ID,
		})
		if errors.Is(versionErr, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeBadRequest, nil, "skill has no valid version to distribute")
		}
		if versionErr != nil {
			return nil, oops.E(oops.CodeUnexpected, versionErr, "resolve latest valid skill version").LogError(ctx, logger)
		}
		resolvedVersionID = version
	}

	existing, err := queries.GetActiveSkillDistributionRecord(ctx, repo.GetActiveSkillDistributionRecordParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   skill.ID,
		PluginID:  uuid.NullUUID{UUID: pluginID, Valid: true},
	})
	if err == nil {
		distribution := existing.SkillDistribution
		if distribution.PinnedVersionID == pinnedVersionID {
			if err := dbtx.Commit(ctx); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "commit unchanged skill distribution transaction").LogError(ctx, logger)
			}
			return mv.BuildSkillDistributionView(distribution, skill.Name, skill.DisplayName, plugin.Name, resolvedVersionID), nil
		}

		beforeSnapshot := buildSkillDistributionAuditSnapshot(distribution, existing.ResolvedVersionID)
		distribution, err = queries.UpdateSkillDistribution(ctx, repo.UpdateSkillDistributionParams{
			PinnedVersionID: pinnedVersionID,
			ProjectID:       *authCtx.ProjectID,
			SkillID:         skill.ID,
			PluginID:        uuid.NullUUID{UUID: pluginID, Valid: true},
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "update skill distribution").LogError(ctx, logger)
		}
		if err := s.audit.LogSkillUpdateDistribution(ctx, dbtx, audit.LogSkillUpdateDistributionEvent{
			OrganizationID:             authCtx.ActiveOrganizationID,
			ProjectID:                  *authCtx.ProjectID,
			Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:           authCtx.Email,
			ActorSlug:                  nil,
			SkillURN:                   urn.NewSkill(skill.ID),
			SkillName:                  skill.Name,
			SkillDisplayName:           skill.DisplayName,
			DistributionSnapshotBefore: beforeSnapshot,
			DistributionSnapshotAfter:  buildSkillDistributionAuditSnapshot(distribution, resolvedVersionID),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log skill distribution update").LogError(ctx, logger)
		}
		if err := dbtx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "commit skill distribution update transaction").LogError(ctx, logger)
		}
		return mv.BuildSkillDistributionView(distribution, skill.Name, skill.DisplayName, plugin.Name, resolvedVersionID), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "get active skill distribution").LogError(ctx, logger)
	}

	distribution, err := queries.CreateSkillDistribution(ctx, repo.CreateSkillDistributionParams{
		PinnedVersionID: pinnedVersionID,
		PluginID:        pluginID,
		CreatedByUserID: authCtx.UserID,
		ProjectID:       *authCtx.ProjectID,
		SkillID:         skill.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create skill distribution").LogError(ctx, logger)
	}
	if err := s.audit.LogSkillDistribute(ctx, dbtx, audit.LogSkillDistributeEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		SkillURN:                  urn.NewSkill(skill.ID),
		SkillName:                 skill.Name,
		SkillDisplayName:          skill.DisplayName,
		DistributionSnapshotAfter: buildSkillDistributionAuditSnapshot(distribution, resolvedVersionID),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log skill distribution create").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit distribute skill transaction").LogError(ctx, logger)
	}

	return mv.BuildSkillDistributionView(distribution, skill.Name, skill.DisplayName, plugin.Name, resolvedVersionID), nil
}

func (s *Service) Undistribute(ctx context.Context, payload *gen.UndistributePayload) error {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
		return err
	}
	skillID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, nil, "invalid plugin id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin undistribute skill transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	skill, err := queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{ProjectID: *authCtx.ProjectID, ID: skillID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "skill not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "lock skill for undistribution").LogError(ctx, logger)
	}
	existing, err := queries.GetActiveSkillDistributionRecord(ctx, repo.GetActiveSkillDistributionRecordParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   skill.ID,
		PluginID:  uuid.NullUUID{UUID: pluginID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if err := dbtx.Commit(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "commit missing skill distribution transaction").LogError(ctx, logger)
		}
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get skill distribution for revocation").LogError(ctx, logger)
	}

	distribution := existing.SkillDistribution
	beforeSnapshot := buildSkillDistributionAuditSnapshot(distribution, existing.ResolvedVersionID)
	revoked, err := queries.RevokeActiveSkillDistribution(ctx, repo.RevokeActiveSkillDistributionParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   skill.ID,
		PluginID:  uuid.NullUUID{UUID: pluginID, Valid: true},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke skill distribution").LogError(ctx, logger)
	}
	if err := s.audit.LogSkillUndistribute(ctx, dbtx, audit.LogSkillUndistributeEvent{
		OrganizationID:             authCtx.ActiveOrganizationID,
		ProjectID:                  *authCtx.ProjectID,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:           authCtx.Email,
		ActorSlug:                  nil,
		SkillURN:                   urn.NewSkill(skill.ID),
		SkillName:                  skill.Name,
		SkillDisplayName:           skill.DisplayName,
		DistributionSnapshotBefore: beforeSnapshot,
		DistributionSnapshotAfter:  buildSkillDistributionAuditSnapshot(revoked, existing.ResolvedVersionID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log skill undistribution").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit undistribute skill transaction").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) ListDistributions(ctx context.Context, payload *gen.ListDistributionsPayload) (*gen.ListSkillDistributionsResult, error) {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillRead)
	if err != nil {
		return nil, err
	}

	skillID, err := conv.PtrToNullUUID(payload.SkillID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	pluginID, err := conv.PtrToNullUUID(payload.PluginID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid plugin id")
	}

	cursorCreatedAt := conv.PtrToPGTimestamptz(nil)
	cursorID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.Cursor != nil {
		createdAt, id, decodeErr := decodeCreatedAtIDCursor(*payload.Cursor)
		if decodeErr != nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill distribution cursor")
		}
		cursorCreatedAt = conv.ToPGTimestamptz(createdAt)
		cursorID = uuid.NullUUID{UUID: id, Valid: true}
	}

	rows, err := repo.New(s.db).ListActiveSkillDistributions(ctx, repo.ListActiveSkillDistributionsParams{
		ProjectID:       *authCtx.ProjectID,
		SkillID:         skillID,
		PluginID:        pluginID,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		PageLimit:       conv.SafeInt32(payload.Limit + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list skill distributions").LogError(ctx, logger)
	}

	hasMore := len(rows) > payload.Limit
	if hasMore {
		rows = rows[:payload.Limit]
	}
	var nextCursor *string
	if hasMore {
		last := rows[len(rows)-1]
		encoded := encodeCreatedAtIDCursor(last.SkillDistribution.CreatedAt.Time, last.SkillDistribution.ID)
		nextCursor = &encoded
	}

	return &gen.ListSkillDistributionsResult{
		Distributions: mv.BuildSkillDistributionListView(rows),
		NextCursor:    nextCursor,
	}, nil
}

func (s *Service) Archive(ctx context.Context, payload *gen.ArchivePayload) error {
	authCtx, logger, err := s.requireAccess(ctx, authz.ScopeSkillWrite)
	if err != nil {
		return err
	}

	skillID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin archive skill transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	name, err := queries.GetSkillName(ctx, repo.GetSkillNameParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skillID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if err := dbtx.Commit(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "commit missing skill archive transaction").LogError(ctx, logger)
		}
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get skill name for archive").LogError(ctx, logger)
	}
	if err := queries.LockSkillName(ctx, repo.LockSkillNameParams{
		ProjectID: *authCtx.ProjectID,
		Name:      name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock skill name for archive").LogError(ctx, logger)
	}

	skill, err := queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skillID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if err := dbtx.Commit(ctx); err != nil {
			return oops.E(oops.CodeUnexpected, err, "commit concurrent skill archive transaction").LogError(ctx, logger)
		}
		return nil
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get skill for archive").LogError(ctx, logger)
	}

	latestID, count, err := loadDerivedSkillState(ctx, queries, *authCtx.ProjectID, skill.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load skill state before archive").LogError(ctx, logger)
	}
	beforeSnapshot := buildSkillAuditSnapshot(skill, latestID, count)

	revokedDistributions, err := queries.RevokeAllSkillDistributionsBySkill(ctx, repo.RevokeAllSkillDistributionsBySkillParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   skill.ID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "revoke skill distributions during archive").LogError(ctx, logger)
	}
	for _, revoked := range revokedDistributions {
		beforeDistribution := buildSkillDistributionAuditSnapshot(revoked.SkillDistribution, revoked.ResolvedVersionID)
		beforeDistribution.RevokedAt = nil
		beforeDistribution.UpdatedAt = conv.FromPGTimestamptz(revoked.PreviousUpdatedAt)
		if auditErr := s.audit.LogSkillUndistribute(ctx, dbtx, audit.LogSkillUndistributeEvent{
			OrganizationID:             authCtx.ActiveOrganizationID,
			ProjectID:                  *authCtx.ProjectID,
			Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:           authCtx.Email,
			ActorSlug:                  nil,
			SkillURN:                   urn.NewSkill(skill.ID),
			SkillName:                  skill.Name,
			SkillDisplayName:           skill.DisplayName,
			DistributionSnapshotBefore: beforeDistribution,
			DistributionSnapshotAfter:  buildSkillDistributionAuditSnapshot(revoked.SkillDistribution, revoked.ResolvedVersionID),
		}); auditErr != nil {
			return oops.E(oops.CodeUnexpected, auditErr, "log archived skill undistribution").LogError(ctx, logger)
		}
	}

	archived, err := queries.ArchiveSkill(ctx, repo.ArchiveSkillParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skill.ID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "archive skill").LogError(ctx, logger)
	}
	afterSnapshot := buildSkillAuditSnapshot(archived, latestID, count)

	if err := s.audit.LogSkillArchive(ctx, dbtx, audit.LogSkillArchiveEvent{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Actor:               urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:    authCtx.Email,
		ActorSlug:           nil,
		SkillURN:            urn.NewSkill(archived.ID),
		SkillName:           archived.Name,
		SkillDisplayName:    archived.DisplayName,
		SkillSnapshotBefore: beforeSnapshot,
		SkillSnapshotAfter:  afterSnapshot,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log skill archive").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit archive skill transaction").LogError(ctx, logger)
	}

	return nil
}
