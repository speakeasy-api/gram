package skills

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/gen/http/skills/server"
	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

const (
	maxCaptureArtifactMiB    = 10
	maxCaptureArtifactBytes  = maxCaptureArtifactMiB * 1024 * 1024
	maxSkillNameLen          = 100
	maxSkillSlugLen          = 100
	skillAssetKind           = "skill"
	skillVersionStatePending = "pending_review"
	skillVersionStateActive  = "active"
	skillAssetFormatZip      = "zip"
)

var allowedCaptureContentTypes = map[string]struct{}{
	"application/zip":              {},
	"application/x-zip-compressed": {},
	"application/x-zip":            {},
}

type CaptureMode string

const (
	CaptureModeDisabled       CaptureMode = "disabled"
	CaptureModeProjectOnly    CaptureMode = "project_only"
	CaptureModeUserOnly       CaptureMode = "user_only"
	CaptureModeProjectAndUser CaptureMode = "project_and_user"
)

type Service struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	auth       *auth.Auth
	authz      *authz.Engine
	cache      cache.Cache
	db         *pgxpool.Pool
	storage    assets.BlobStore
	repo       *assetsrepo.Queries
	skillsRepo *skillsrepo.Queries
	features   *productfeatures.Client
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionsMgr *sessions.Manager,
	cacheAdapter cache.Cache,
	storage assets.BlobStore,
	authzEngine *authz.Engine,
	features *productfeatures.Client,
) *Service {
	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/skills"),
		logger:     logger.With(attr.SlogComponent("skills")),
		auth:       auth.New(logger, db, sessionsMgr, authzEngine),
		authz:      authzEngine,
		cache:      cacheAdapter,
		db:         db,
		storage:    storage,
		repo:       assetsrepo.New(db),
		skillsRepo: skillsrepo.New(db),
		features:   features,
	}
}

func (s *Service) requireSkillsCaptureEnabled(ctx context.Context, organizationID string) error {
	if organizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	if s.features == nil {
		return nil
	}

	enabled, err := s.features.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureSkillsCapture)
	if err != nil {
		return fmt.Errorf("check skills capture feature: %w", err)
	}
	if !enabled {
		return oops.E(oops.CodeForbidden, nil, "skills capture is not enabled for this organization")
	}

	return nil
}

func (s *Service) recordCaptureAttempt(
	ctx context.Context,
	repo *skillsrepo.Queries,
	organizationID string,
	projectID uuid.UUID,
	userID string,
	skillSlug string,
	payload *gen.CaptureSkillProducerForm,
	outcome string,
	reason string,
	skillID uuid.NullUUID,
	versionID uuid.NullUUID,
	assetID uuid.NullUUID,
) error {
	if payload == nil {
		return nil
	}

	_, err := repo.CreateSkillsCaptureAttempt(ctx, skillsrepo.CreateSkillsCaptureAttemptParams{
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		CapturedByUserID: userID,
		SkillName:        conv.PtrToPGTextEmpty(conv.PtrEmpty(payload.Name)),
		SkillSlug:        conv.PtrToPGTextEmpty(conv.PtrEmpty(skillSlug)),
		Scope:            payload.Scope,
		DiscoveryRoot:    payload.DiscoveryRoot,
		SourceType:       payload.SourceType,
		ResolutionStatus: payload.ResolutionStatus,
		ContentSha256:    conv.PtrToPGTextEmpty(conv.PtrEmpty(payload.ContentSha256)),
		AssetFormat:      conv.PtrToPGTextEmpty(conv.PtrEmpty(payload.AssetFormat)),
		ContentLength: func() pgtype.Int8 {
			if payload.ContentLength <= 0 {
				return pgtype.Int8{Int64: 0, Valid: false}
			}
			return pgtype.Int8{Int64: payload.ContentLength, Valid: true}
		}(),
		Outcome:        outcome,
		Reason:         reason,
		SkillID:        skillID,
		SkillVersionID: versionID,
		AssetID:        assetID,
	})
	if err != nil {
		return fmt.Errorf("create skills capture attempt: %w", err)
	}
	return nil
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	server.Mount(
		mux,
		server.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func hookSessionCacheKey(sessionID string) string {
	return fmt.Sprintf("session:metadata:%s", sessionID)
}

type claudeValidatedSessionMetadata struct {
	SessionID   string
	ServiceName string
	UserEmail   string
	ClaudeOrgID string
	GramOrgID   string
	ProjectID   string
}

func (s *Service) authorizeClaudeValidatedSession(ctx context.Context, claudeSessionID, projectSlug string) (context.Context, error) {
	if s.cache == nil {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "validated Claude session capture not configured")
	}
	if claudeSessionID == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	var metadata claudeValidatedSessionMetadata
	if err := s.cache.Get(ctx, hookSessionCacheKey(claudeSessionID), &metadata); err != nil {
		return ctx, oops.E(oops.CodeUnauthorized, err, "validated Claude session not found")
	}
	if metadata.GramOrgID == "" || metadata.ProjectID == "" {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "validated Claude session missing project context")
	}

	projectID, err := uuid.Parse(metadata.ProjectID)
	if err != nil {
		return ctx, oops.E(oops.CodeUnauthorized, err, "invalid validated Claude session project")
	}

	captureUserID := metadata.UserEmail
	if captureUserID == "" {
		captureUserID = metadata.SessionID
	}

	authCtx := &contextvalues.AuthContext{
		SessionID:             &metadata.SessionID,
		UserID:                captureUserID,
		ActiveOrganizationID:  metadata.GramOrgID,
		OrganizationSlug:      "",
		ProjectID:             &projectID,
		ProjectSlug:           conv.PtrEmpty(projectSlug),
		Email:                 conv.PtrEmpty(metadata.UserEmail),
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyScopes:          []string{"hooks"},
		IsAdmin:               false,
	}

	ctx = contextvalues.SetAuthContext(ctx, authCtx)
	if projectSlug != "" {
		ctx, err = s.auth.Authorize(ctx, projectSlug, &security.APIKeyScheme{Name: "project_slug", Scopes: []string{}, RequiredScopes: []string{}})
		if err != nil {
			return ctx, err
		}
	}

	return ctx, nil
}

func (s *Service) Get(ctx context.Context, payload *gen.GetPayload) (*gen.Skill, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	skill, err := s.skillsRepo.GetSkillBySlug(ctx, skillsrepo.GetSkillBySlugParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      string(payload.Slug),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}

		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill by slug: %w", err), "error loading skill").Log(ctx, s.logger)
	}

	return buildSkill(skill), nil
}

func (s *Service) List(ctx context.Context, _ *gen.ListPayload) (*gen.ListSkillsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	rows, err := s.skillsRepo.ListSkillsWithActiveVersion(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("list skills: %w", err), "error listing skills").Log(ctx, s.logger)
	}

	skills := make([]*gen.SkillEntry, 0, len(rows))
	for _, row := range rows {
		skills = append(skills, buildSkillEntry(row))
	}

	return &gen.ListSkillsResult{
		Skills: skills,
	}, nil
}

func (s *Service) GetSettings(ctx context.Context, _ *gen.GetSettingsPayload) (*gen.SkillCaptureSettings, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	settings, err := s.getCaptureSettings(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	return settings, nil
}

func (s *Service) SetSettings(ctx context.Context, payload *gen.SetSettingsPayload) (*gen.SkillCaptureSettings, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	settings, err := s.getCaptureSettings(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	mode := captureModeFromSettings(payload.Enabled, payload.CaptureProjectSkills, payload.CaptureUserSkills)
	orgDefaultMode := CaptureModeDisabled
	if settings.OrgDefaultMode != nil {
		orgDefaultMode, err = parseCaptureMode(*settings.OrgDefaultMode)
		if err != nil {
			return nil, err
		}
	}

	if mode == orgDefaultMode {
		if !settings.InheritedFromOrganization {
			if _, err := s.skillsRepo.DeleteProjectCapturePolicyOverride(ctx, skillsrepo.DeleteProjectCapturePolicyOverrideParams{
				OrganizationID: authCtx.ActiveOrganizationID,
				ProjectID:      *authCtx.ProjectID,
			}); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("delete capture policy override: %w", err), "error saving capture policy")
			}
		}
	} else {
		if _, err := s.skillsRepo.UpsertProjectCapturePolicyOverride(ctx, skillsrepo.UpsertProjectCapturePolicyOverrideParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			Mode:           string(mode),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("set capture policy override: %w", err), "error saving capture policy")
		}
	}

	settings, err = s.getCaptureSettings(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	return settings, nil
}

func (s *Service) ListVersions(ctx context.Context, payload *gen.ListVersionsPayload) (*gen.ListSkillVersionsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	skillID, err := uuid.Parse(payload.SkillID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid skill id")
	}

	versions, err := s.skillsRepo.ListSkillVersions(ctx, skillsrepo.ListSkillVersionsParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   skillID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("list skill versions: %w", err), "error loading skill versions").Log(ctx, s.logger)
	}

	result := &gen.ListSkillVersionsResult{Versions: make([]*gen.SkillVersion, 0, len(versions))}
	for i := range versions {
		result.Versions = append(result.Versions, buildSkillVersion(&versions[i]))
	}
	return result, nil
}

func (s *Service) ListPending(ctx context.Context, _ *gen.ListPendingPayload) (*gen.ListPendingSkillsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	rows, err := s.skillsRepo.ListPendingSkillVersions(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("list pending skill versions: %w", err), "error loading pending skills").Log(ctx, s.logger)
	}

	pendingBySkillID := map[uuid.UUID]*gen.PendingSkillEntry{}
	orderedSkillIDs := make([]uuid.UUID, 0)
	for i := range rows {
		entry, exists := pendingBySkillID[rows[i].Skill.ID]
		if !exists {
			entry = &gen.PendingSkillEntry{
				Skill:    buildSkill(rows[i].Skill),
				Versions: make([]*gen.SkillVersion, 0),
			}
			pendingBySkillID[rows[i].Skill.ID] = entry
			orderedSkillIDs = append(orderedSkillIDs, rows[i].Skill.ID)
		}
		entry.Versions = append(entry.Versions, buildSkillVersion(&rows[i].SkillVersion))
	}

	result := &gen.ListPendingSkillsResult{Skills: make([]*gen.PendingSkillEntry, 0, len(orderedSkillIDs))}
	for _, skillID := range orderedSkillIDs {
		result.Skills = append(result.Skills, pendingBySkillID[skillID])
	}

	return result, nil
}

func (s *Service) ApproveVersion(ctx context.Context, payload *gen.ApproveVersionPayload) (*gen.SkillVersion, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	versionID, err := uuid.Parse(payload.VersionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid version id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing resource").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	repo := skillsrepo.New(dbtx)
	version, err := repo.GetSkillVersion(ctx, skillsrepo.GetSkillVersionParams{
		ProjectID: *authCtx.ProjectID,
		ID:        versionID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill version: %w", err), "error loading skill version").Log(ctx, s.logger)
	}
	if version.State != skillVersionStatePending {
		return nil, oops.E(oops.CodeConflict, nil, "skill version must be pending review to approve")
	}

	versions, err := repo.ListSkillVersions(ctx, skillsrepo.ListSkillVersionsParams{
		ProjectID: *authCtx.ProjectID,
		SkillID:   version.SkillID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("list skill versions for approval: %w", err), "error approving skill version").Log(ctx, s.logger)
	}
	for i := range versions {
		if versions[i].State == "active" && versions[i].ID != version.ID {
			if _, err := repo.UpdateSkillVersionState(ctx, skillsrepo.UpdateSkillVersionStateParams{
				State:     "superseded",
				ID:        versions[i].ID,
				ProjectID: *authCtx.ProjectID,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("supersede existing active version: %w", err), "error approving skill version").Log(ctx, s.logger)
			}
		}
	}

	version, err = repo.UpdateSkillVersionState(ctx, skillsrepo.UpdateSkillVersionStateParams{
		State:     "active",
		ID:        version.ID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, nil, "another skill version is already active")
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("mark skill version active: %w", err), "error approving skill version").Log(ctx, s.logger)
	}

	if _, err := repo.SetSkillActiveVersion(ctx, skillsrepo.SetSkillActiveVersionParams{
		ActiveVersionID: uuid.NullUUID{UUID: version.ID, Valid: true},
		ProjectID:       *authCtx.ProjectID,
		ID:              version.SkillID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("set active skill version: %w", err), "error approving skill version").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error approving skill version").Log(ctx, s.logger)
	}

	return buildSkillVersion(&version), nil
}

func (s *Service) SupersedeVersion(ctx context.Context, payload *gen.SupersedeVersionPayload) (*gen.SkillVersion, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	versionID, err := uuid.Parse(payload.VersionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid version id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing resource").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	repo := skillsrepo.New(dbtx)
	version, err := repo.GetSkillVersion(ctx, skillsrepo.GetSkillVersionParams{
		ProjectID: *authCtx.ProjectID,
		ID:        versionID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill version for supersede: %w", err), "error superseding skill version").Log(ctx, s.logger)
	}
	if version.State != skillVersionStatePending && version.State != skillVersionStateActive {
		return nil, oops.E(oops.CodeConflict, nil, "skill version must be pending review or active to supersede")
	}

	version, err = repo.UpdateSkillVersionState(ctx, skillsrepo.UpdateSkillVersionStateParams{
		State:     "superseded",
		ID:        versionID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("supersede skill version: %w", err), "error superseding skill version").Log(ctx, s.logger)
	}

	skill, err := repo.GetSkill(ctx, skillsrepo.GetSkillParams{
		ProjectID: *authCtx.ProjectID,
		ID:        version.SkillID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill for supersede: %w", err), "error superseding skill version").Log(ctx, s.logger)
	}
	if skill.ActiveVersionID.Valid && skill.ActiveVersionID.UUID == version.ID {
		if _, err := repo.ClearSkillActiveVersion(ctx, skillsrepo.ClearSkillActiveVersionParams{
			ProjectID: *authCtx.ProjectID,
			ID:        skill.ID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("clear active skill version: %w", err), "error superseding skill version").Log(ctx, s.logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error superseding skill version").Log(ctx, s.logger)
	}

	return buildSkillVersion(&version), nil
}

func (s *Service) RejectVersion(ctx context.Context, payload *gen.RejectVersionPayload) (*gen.SkillVersion, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	versionID, err := uuid.Parse(payload.VersionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid version id")
	}

	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "reject reason is required")
	}
	if utf8.RuneCountInString(reason) > 2000 {
		return nil, oops.E(oops.CodeBadRequest, nil, "reject reason exceeds 2000 characters")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing resource").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	repo := skillsrepo.New(dbtx)
	version, err := repo.GetSkillVersion(ctx, skillsrepo.GetSkillVersionParams{
		ProjectID: *authCtx.ProjectID,
		ID:        versionID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill version for reject: %w", err), "error rejecting skill version").Log(ctx, s.logger)
	}
	if version.State != skillVersionStatePending {
		return nil, oops.E(oops.CodeConflict, nil, "skill version must be pending review to reject")
	}

	version, err = repo.RejectSkillVersion(ctx, skillsrepo.RejectSkillVersionParams{
		RejectedByUserID: conv.PtrToPGTextEmpty(conv.PtrEmpty(authCtx.UserID)),
		RejectedReason:   conv.PtrToPGTextEmpty(conv.PtrEmpty(reason)),
		ID:               versionID,
		ProjectID:        *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, nil, "skill version must be pending review to reject")
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("reject skill version: %w", err), "error rejecting skill version").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error rejecting skill version").Log(ctx, s.logger)
	}

	return buildSkillVersion(&version), nil
}

func (s *Service) Archive(ctx context.Context, payload *gen.ArchivePayload) (*gen.Skill, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	skillID, err := uuid.Parse(payload.SkillID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid skill id")
	}

	skill, err := s.skillsRepo.ArchiveSkill(ctx, skillsrepo.ArchiveSkillParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skillID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("archive skill: %w", err), "error archiving skill").Log(ctx, s.logger)
	}

	return buildSkill(skill), nil
}

func (s *Service) captureWithAuthorizedContext(ctx context.Context, authCtx *contextvalues.AuthContext, payload *gen.CaptureSkillProducerForm, reader io.ReadCloser) (*gen.CaptureSkillResult, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	skillSlug := conv.ToSlug(payload.Name)
	if err := s.requireSkillsCaptureEnabled(ctx, authCtx.ActiveOrganizationID); err != nil {
		if recordErr := s.recordCaptureAttempt(
			ctx,
			s.skillsRepo,
			authCtx.ActiveOrganizationID,
			*authCtx.ProjectID,
			authCtx.UserID,
			skillSlug,
			payload,
			"rejected",
			"feature_disabled",
			uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		); recordErr != nil {
			s.logger.WarnContext(ctx, "failed to record capture attempt", attr.SlogError(recordErr))
		}
		return nil, err
	}

	effectiveMode, err := s.getEffectiveCaptureMode(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}
	if !captureModeAllowsScope(effectiveMode, payload.Scope) {
		reason := "scope_not_permitted"
		if effectiveMode == CaptureModeDisabled {
			reason = "policy_disabled"
			if recordErr := s.recordCaptureAttempt(
				ctx,
				s.skillsRepo,
				authCtx.ActiveOrganizationID,
				*authCtx.ProjectID,
				authCtx.UserID,
				skillSlug,
				payload,
				"rejected",
				reason,
				uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			); recordErr != nil {
				s.logger.WarnContext(ctx, "failed to record capture attempt", attr.SlogError(recordErr))
			}
			return nil, oops.E(oops.CodeForbidden, nil, "skill capture is disabled")
		}
		if recordErr := s.recordCaptureAttempt(
			ctx,
			s.skillsRepo,
			authCtx.ActiveOrganizationID,
			*authCtx.ProjectID,
			authCtx.UserID,
			skillSlug,
			payload,
			"rejected",
			reason,
			uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		); recordErr != nil {
			s.logger.WarnContext(ctx, "failed to record capture attempt", attr.SlogError(recordErr))
		}
		return nil, oops.E(
			oops.CodeForbidden,
			nil,
			"skill capture scope %s is not permitted by effective mode %s",
			payload.Scope,
			effectiveMode,
		)
	}

	if payload.ContentLength <= 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length must be > 0")
	}
	if payload.ContentLength > maxCaptureArtifactBytes {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length exceeds %d MiB limit", maxCaptureArtifactMiB)
	}

	mediaType, _, err := mime.ParseMediaType(payload.ContentType)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("parse content type: %w", err), "invalid content type")
	}
	if _, exists := allowedCaptureContentTypes[mediaType]; !exists {
		return nil, oops.E(oops.CodeUnsupportedMedia, nil, "unsupported content type: %s", mediaType)
	}

	if len(payload.Name) > maxSkillNameLen {
		return nil, oops.E(oops.CodeBadRequest, nil, "skill name exceeds %d characters", maxSkillNameLen)
	}

	if skillSlug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "skill name must include at least one alphanumeric character")
	}
	if len(skillSlug) > maxSkillSlugLen {
		return nil, oops.E(oops.CodeBadRequest, nil, "skill slug exceeds %d characters", maxSkillSlugLen)
	}
	if payload.AssetFormat != skillAssetFormatZip {
		return nil, oops.E(oops.CodeBadRequest, nil, "unsupported asset format: %s", payload.AssetFormat)
	}

	file, err := os.CreateTemp("", "skill-capture-*")
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create temp file: %w", err), "error buffering skill artifact")
	}
	defer o11y.NoLogDefer(func() error {
		_ = file.Close()
		return os.Remove(file.Name())
	})

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, h), io.LimitReader(reader, payload.ContentLength+1))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("copy artifact bytes: %w", err), "error buffering skill artifact")
	}
	if n != payload.ContentLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "content length mismatch")
	}

	contentSHA := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(contentSHA, payload.ContentSha256) {
		return nil, oops.E(oops.CodeBadRequest, nil, "content sha256 mismatch")
	}

	existing, err := s.findExistingAsset(ctx, *authCtx.ProjectID, contentSHA)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		existingAssetID, err := uuid.Parse(existing.ID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse existing asset id: %w", err), "error loading skill artifact")
		}

		dbtx, err := s.db.Begin(ctx)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error accessing skill assets").Log(ctx, s.logger)
		}
		txClosed := false
		defer o11y.NoLogDefer(func() error {
			if txClosed || dbtx == nil {
				return nil
			}
			return dbtx.Rollback(ctx)
		})

		skill, version, err := s.ensureSkillLineageForCapture(ctx, s.skillsRepo.WithTx(dbtx), authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, skillSlug, contentSHA, payload, existingAssetID)
		if err != nil {
			return nil, err
		}
		if err := s.recordCaptureAttempt(
			ctx,
			s.skillsRepo.WithTx(dbtx),
			authCtx.ActiveOrganizationID,
			*authCtx.ProjectID,
			authCtx.UserID,
			skillSlug,
			payload,
			"duplicate",
			"existing_asset",
			uuid.NullUUID{UUID: skill.ID, Valid: true},
			uuid.NullUUID{UUID: version.ID, Valid: true},
			uuid.NullUUID{UUID: existingAssetID, Valid: true},
		); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error recording capture attempt").Log(ctx, s.logger)
		}

		if err := dbtx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to save skill artifact").Log(ctx, s.logger)
		}
		txClosed = true

		return &gen.CaptureSkillResult{Asset: existing}, nil
	}

	filename := fmt.Sprintf("skill-%s.zip", contentSHA)
	uri, err := s.uploadAsset(ctx, *authCtx.ProjectID, filename, mediaType, payload.ContentLength, file)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing skill assets").Log(ctx, s.logger)
	}
	txClosed := false
	defer o11y.NoLogDefer(func() error {
		if txClosed || dbtx == nil {
			return nil
		}
		return dbtx.Rollback(ctx)
	})

	skillsTxRepo := s.skillsRepo.WithTx(dbtx)
	asset, err := s.repo.WithTx(dbtx).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     *authCtx.ProjectID,
		Sha256:        contentSHA,
		Kind:          skillAssetKind,
		ContentType:   mediaType,
		ContentLength: payload.ContentLength,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			existing, findErr := s.findExistingAsset(ctx, *authCtx.ProjectID, contentSHA)
			if findErr != nil {
				return nil, findErr
			}
			if existing == nil {
				return nil, oops.E(oops.CodeConflict, nil, "skill asset already exists with incompatible metadata")
			}

			existingAssetID, parseErr := uuid.Parse(existing.ID)
			if parseErr != nil {
				return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("parse existing asset id: %w", parseErr), "error loading skill artifact")
			}

			if rollbackErr := dbtx.Rollback(ctx); rollbackErr != nil {
				return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("rollback failed asset transaction: %w", rollbackErr), "failed to save skill artifact").Log(ctx, s.logger)
			}

			dbtx, err = s.db.Begin(ctx)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error accessing skill assets").Log(ctx, s.logger)
			}
			skillsTxRepo = s.skillsRepo.WithTx(dbtx)

			skill, version, err := s.ensureSkillLineageForCapture(ctx, skillsTxRepo, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, skillSlug, contentSHA, payload, existingAssetID)
			if err != nil {
				return nil, err
			}
			if err := s.recordCaptureAttempt(
				ctx,
				skillsTxRepo,
				authCtx.ActiveOrganizationID,
				*authCtx.ProjectID,
				authCtx.UserID,
				skillSlug,
				payload,
				"duplicate",
				"asset_conflict_reused",
				uuid.NullUUID{UUID: skill.ID, Valid: true},
				uuid.NullUUID{UUID: version.ID, Valid: true},
				uuid.NullUUID{UUID: existingAssetID, Valid: true},
			); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error recording capture attempt").Log(ctx, s.logger)
			}

			if err := dbtx.Commit(ctx); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to save skill artifact").Log(ctx, s.logger)
			}
			txClosed = true

			return &gen.CaptureSkillResult{Asset: existing}, nil
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create skill asset: %w", err), "error saving skill artifact")
	}
	if asset.Kind != skillAssetKind {
		return nil, oops.E(oops.CodeConflict, nil, "skill asset hash conflicts with existing non-skill asset")
	}

	skill, version, err := s.ensureSkillLineageForCapture(ctx, skillsTxRepo, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, skillSlug, contentSHA, payload, asset.ID)
	if err != nil {
		return nil, err
	}
	if err := s.recordCaptureAttempt(
		ctx,
		skillsTxRepo,
		authCtx.ActiveOrganizationID,
		*authCtx.ProjectID,
		authCtx.UserID,
		skillSlug,
		payload,
		"accepted",
		"captured",
		uuid.NullUUID{UUID: skill.ID, Valid: true},
		uuid.NullUUID{UUID: version.ID, Valid: true},
		uuid.NullUUID{UUID: asset.ID, Valid: true},
	); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording capture attempt").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save skill artifact").Log(ctx, s.logger)
	}
	txClosed = true

	return &gen.CaptureSkillResult{
		Asset: &gen.CaptureSkillAsset{
			ID:            asset.ID.String(),
			Kind:          asset.Kind,
			Sha256:        asset.Sha256,
			ContentType:   asset.ContentType,
			ContentLength: asset.ContentLength,
			CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) Capture(ctx context.Context, payload *gen.CaptureSkillProducerForm, reader io.ReadCloser) (*gen.CaptureSkillResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		defer o11y.LogDefer(ctx, s.logger, func() error {
			return reader.Close()
		})
		return nil, oops.C(oops.CodeUnauthorized)
	}

	return s.captureWithAuthorizedContext(ctx, authCtx, payload, reader)
}

func (s *Service) CaptureClaude(ctx context.Context, payload *gen.CaptureClaudePayload, reader io.ReadCloser) (*gen.CaptureSkillResult, error) {
	authorizedCtx, err := s.authorizeClaudeValidatedSession(ctx, payload.ClaudeSessionID, "")
	if err != nil {
		defer o11y.LogDefer(ctx, s.logger, func() error {
			return reader.Close()
		})
		return nil, err
	}
	authCtx, ok := contextvalues.GetAuthContext(authorizedCtx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		defer o11y.LogDefer(ctx, s.logger, func() error {
			return reader.Close()
		})
		return nil, oops.C(oops.CodeUnauthorized)
	}

	capturePayload := &gen.CaptureSkillProducerForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             payload.Name,
		Scope:            payload.Scope,
		DiscoveryRoot:    payload.DiscoveryRoot,
		SourceType:       payload.SourceType,
		ContentSha256:    payload.ContentSha256,
		AssetFormat:      payload.AssetFormat,
		ResolutionStatus: payload.ResolutionStatus,
		SkillID:          payload.SkillID,
		SkillVersionID:   payload.SkillVersionID,
		ContentType:      payload.ContentType,
		ContentLength:    payload.ContentLength,
	}

	return s.captureWithAuthorizedContext(authorizedCtx, authCtx, capturePayload, reader)
}

func (s *Service) UploadManual(ctx context.Context, payload *gen.UploadManualPayload, reader io.ReadCloser) (*gen.CaptureSkillResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		defer o11y.LogDefer(ctx, s.logger, func() error {
			return reader.Close()
		})
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	capturePayload := &gen.CaptureSkillProducerForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Name:             payload.Name,
		Scope:            payload.Scope,
		DiscoveryRoot:    payload.DiscoveryRoot,
		SourceType:       payload.SourceType,
		ContentSha256:    payload.ContentSha256,
		AssetFormat:      payload.AssetFormat,
		ResolutionStatus: payload.ResolutionStatus,
		SkillID:          payload.SkillID,
		SkillVersionID:   payload.SkillVersionID,
		ContentType:      payload.ContentType,
		ContentLength:    payload.ContentLength,
	}

	return s.captureWithAuthorizedContext(ctx, authCtx, capturePayload, reader)
}

func (s *Service) getCaptureSettings(ctx context.Context, organizationID string, projectID uuid.UUID) (*gen.SkillCaptureSettings, error) {
	if organizationID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "missing organization context")
	}

	row, err := s.skillsRepo.GetCaptureSettings(ctx, skillsrepo.GetCaptureSettingsParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("get capture settings: %w", err), "error loading capture policy")
	}

	effectiveMode, err := parseCaptureMode(row.EffectiveMode)
	if err != nil {
		return nil, err
	}

	orgDefaultMode, err := parseOptionalCaptureMode(conv.PtrEmpty(row.OrgDefaultMode))
	if err != nil {
		return nil, err
	}
	projectOverrideMode, err := parseOptionalCaptureMode(conv.PtrEmpty(row.ProjectOverrideMode))
	if err != nil {
		return nil, err
	}

	return buildCaptureSettings(effectiveMode, orgDefaultMode, projectOverrideMode), nil
}

func (s *Service) getEffectiveCaptureMode(ctx context.Context, organizationID string, projectID uuid.UUID) (CaptureMode, error) {
	if organizationID == "" {
		return CaptureModeDisabled, oops.E(oops.CodeUnauthorized, nil, "missing organization context")
	}

	mode, err := s.skillsRepo.GetEffectiveCaptureMode(ctx, skillsrepo.GetEffectiveCaptureModeParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
	})
	if err != nil {
		return CaptureModeDisabled, oops.E(oops.CodeUnexpected, fmt.Errorf("get effective capture mode: %w", err), "error loading capture policy")
	}

	captureMode := CaptureMode(mode)
	switch captureMode {
	case CaptureModeDisabled, CaptureModeProjectOnly, CaptureModeUserOnly, CaptureModeProjectAndUser:
		return captureMode, nil
	default:
		return CaptureModeDisabled, oops.E(oops.CodeUnexpected, nil, "invalid capture policy mode")
	}
}

func (s *Service) ensureSkillLineageForCapture(
	ctx context.Context,
	repo *skillsrepo.Queries,
	organizationID string,
	projectID uuid.UUID,
	userID string,
	skillSlug string,
	contentSHA string,
	payload *gen.CaptureSkillProducerForm,
	assetID uuid.UUID,
) (skillsrepo.Skill, skillsrepo.SkillVersion, error) {
	var (
		skill skillsrepo.Skill
		err   error
	)

	if payload.SkillID != nil && *payload.SkillID != "" {
		skillID, parseErr := uuid.Parse(*payload.SkillID)
		if parseErr != nil {
			return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeBadRequest, parseErr, "invalid skill id")
		}
		skill, err = repo.GetSkill(ctx, skillsrepo.GetSkillParams{
			ProjectID: projectID,
			ID:        skillID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
				return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.C(oops.CodeNotFound)
			}
			return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill by id: %w", err), "error loading skill")
		}
	} else {
		skill, err = repo.GetSkillBySlug(ctx, skillsrepo.GetSkillBySlugParams{
			ProjectID: projectID,
			Slug:      skillSlug,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
				skill, err = repo.CreateSkill(ctx, skillsrepo.CreateSkillParams{
					OrganizationID: organizationID,
					ProjectID:      projectID,
					Name:           payload.Name,
					Slug:           skillSlug,
					Description: pgtype.Text{
						String: "",
						Valid:  false,
					},
					SkillUuid: pgtype.Text{
						String: "",
						Valid:  false,
					},
					ActiveVersionID: uuid.NullUUID{
						UUID:  uuid.Nil,
						Valid: false,
					},
					CreatedByUserID: userID,
				})
				if err != nil {
					var pgErr *pgconn.PgError
					if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
						skill, err = repo.GetSkillBySlug(ctx, skillsrepo.GetSkillBySlugParams{
							ProjectID: projectID,
							Slug:      skillSlug,
						})
						if err != nil {
							return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill by slug after create conflict: %w", err), "error loading skill")
						}
					} else {
						return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeUnexpected, fmt.Errorf("create skill: %w", err), "error saving skill artifact")
					}
				}
			} else {
				return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill by slug: %w", err), "error loading skill")
			}
		}
	}

	version, err := repo.GetSkillVersionByHash(ctx, skillsrepo.GetSkillVersionByHashParams{
		SkillID:       skill.ID,
		ContentSha256: contentSHA,
		ProjectID:     projectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			version, err = repo.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
				AssetID:       assetID,
				ContentSha256: contentSHA,
				AssetFormat:   payload.AssetFormat,
				SizeBytes:     payload.ContentLength,
				SkillBytes: pgtype.Int8{
					Int64: 0,
					Valid: false,
				},
				State:            skillVersionStatePending,
				CapturedByUserID: userID,
				AuthorName: pgtype.Text{
					String: "",
					Valid:  false,
				},
				RejectedByUserID: pgtype.Text{
					String: "",
					Valid:  false,
				},
				RejectedReason: pgtype.Text{
					String: "",
					Valid:  false,
				},
				RejectedAt: pgtype.Timestamptz{
					Time:             time.Time{},
					InfinityModifier: 0,
					Valid:            false,
				},
				FirstSeenTraceID: pgtype.Text{
					String: "",
					Valid:  false,
				},
				FirstSeenSessionID: pgtype.Text{
					String: "",
					Valid:  false,
				},
				FirstSeenAt: pgtype.Timestamptz{
					Time:             time.Time{},
					InfinityModifier: 0,
					Valid:            false,
				},
				SkillID:   skill.ID,
				ProjectID: projectID,
			})
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
					version, err = repo.GetSkillVersionByHash(ctx, skillsrepo.GetSkillVersionByHashParams{
						SkillID:       skill.ID,
						ContentSha256: contentSHA,
						ProjectID:     projectID,
					})
					if err != nil {
						return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill version by hash after create conflict: %w", err), "error loading skill")
					}
				} else {
					return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeUnexpected, fmt.Errorf("create skill version: %w", err), "error saving skill artifact")
				}
			}
		} else {
			return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeUnexpected, fmt.Errorf("get skill version by hash: %w", err), "error loading skill")
		}
	}

	if !skill.ActiveVersionID.Valid {
		_, err = repo.SetSkillActiveVersionIfNull(ctx, skillsrepo.SetSkillActiveVersionIfNullParams{
			ActiveVersionID: uuid.NullUUID{UUID: version.ID, Valid: true},
			ProjectID:       projectID,
			ID:              skill.ID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
				return skill, version, nil
			}
			return skillsrepo.Skill{}, skillsrepo.SkillVersion{}, oops.E(oops.CodeUnexpected, fmt.Errorf("set skill active version when empty: %w", err), "error saving skill artifact")
		}
	}

	return skill, version, nil
}

func captureModeAllowsScope(mode CaptureMode, scope string) bool {
	switch mode {
	case CaptureModeProjectAndUser:
		return scope == "project" || scope == "user"
	case CaptureModeProjectOnly:
		return scope == "project"
	case CaptureModeUserOnly:
		return scope == "user"
	default:
		return false
	}
}

func captureModeFromSettings(enabled bool, captureProjectSkills bool, captureUserSkills bool) CaptureMode {
	if !enabled {
		return CaptureModeDisabled
	}

	switch {
	case captureProjectSkills && captureUserSkills:
		return CaptureModeProjectAndUser
	case captureProjectSkills:
		return CaptureModeProjectOnly
	case captureUserSkills:
		return CaptureModeUserOnly
	default:
		return CaptureModeDisabled
	}
}

func parseCaptureMode(mode string) (CaptureMode, error) {
	captureMode := CaptureMode(mode)
	switch captureMode {
	case CaptureModeDisabled, CaptureModeProjectOnly, CaptureModeUserOnly, CaptureModeProjectAndUser:
		return captureMode, nil
	default:
		return CaptureModeDisabled, oops.E(oops.CodeUnexpected, nil, "invalid capture policy mode")
	}
}

func parseOptionalCaptureMode(mode *string) (*CaptureMode, error) {
	if mode == nil {
		return nil, nil
	}

	parsed, err := parseCaptureMode(*mode)
	if err != nil {
		return nil, err
	}

	return &parsed, nil
}

func buildCaptureSettings(effectiveMode CaptureMode, orgDefaultMode *CaptureMode, projectOverrideMode *CaptureMode) *gen.SkillCaptureSettings {
	enabled, captureProjectSkills, captureUserSkills := captureModeToSettings(effectiveMode)

	return &gen.SkillCaptureSettings{
		EffectiveMode:             string(effectiveMode),
		OrgDefaultMode:            optionalCaptureModeString(orgDefaultMode),
		ProjectOverrideMode:       optionalCaptureModeString(projectOverrideMode),
		Enabled:                   enabled,
		CaptureProjectSkills:      captureProjectSkills,
		CaptureUserSkills:         captureUserSkills,
		InheritedFromOrganization: projectOverrideMode == nil,
	}
}

func captureModeToSettings(mode CaptureMode) (enabled bool, captureProjectSkills bool, captureUserSkills bool) {
	switch mode {
	case CaptureModeProjectAndUser:
		return true, true, true
	case CaptureModeProjectOnly:
		return true, true, false
	case CaptureModeUserOnly:
		return true, false, true
	default:
		return false, false, false
	}
}

func optionalCaptureModeString(mode *CaptureMode) *string {
	if mode == nil {
		return nil
	}

	return conv.PtrEmpty(string(*mode))
}

func (s *Service) archiveStaleSkillAsset(ctx context.Context, projectID uuid.UUID, assetID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE assets
		SET deleted_at = clock_timestamp(), updated_at = clock_timestamp()
		WHERE project_id = $1
		  AND id = $2
		  AND deleted IS FALSE
	`, projectID, assetID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, fmt.Errorf("archive stale skill asset: %w", err), "error loading skill asset")
	}

	return nil
}

func buildSkillEntry(row skillsrepo.ListSkillsWithActiveVersionRow) *gen.SkillEntry {
	return &gen.SkillEntry{
		ID:            row.Skill.ID.String(),
		Name:          row.Skill.Name,
		Slug:          row.Skill.Slug,
		Description:   conv.FromPGText[string](row.Skill.Description),
		SkillUUID:     conv.FromPGText[string](row.Skill.SkillUuid),
		CreatedAt:     row.Skill.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     row.Skill.UpdatedAt.Time.Format(time.RFC3339),
		VersionCount:  row.VersionCount,
		ActiveVersion: buildSkillVersionSummary(row),
	}
}

func buildSkill(skill skillsrepo.Skill) *gen.Skill {
	var activeVersionID *string
	if skill.ActiveVersionID.Valid {
		activeVersionID = conv.PtrEmpty(skill.ActiveVersionID.UUID.String())
	}

	return &gen.Skill{
		ID:              skill.ID.String(),
		Name:            skill.Name,
		Slug:            skill.Slug,
		Description:     conv.FromPGText[string](skill.Description),
		SkillUUID:       conv.FromPGText[string](skill.SkillUuid),
		ActiveVersionID: activeVersionID,
		CreatedAt:       skill.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:       skill.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func buildSkillVersionSummary(row skillsrepo.ListSkillsWithActiveVersionRow) *gen.SkillVersionSummary {
	if !row.ActiveVersionID.Valid {
		return nil
	}

	var firstSeenAt *string
	if row.ActiveVersionFirstSeenAt.Valid {
		firstSeenAt = conv.PtrEmpty(row.ActiveVersionFirstSeenAt.Time.Format(time.RFC3339))
	}

	return &gen.SkillVersionSummary{
		ID:            row.ActiveVersionID.UUID.String(),
		ContentSha256: row.ActiveVersionContentSha256.String,
		AssetFormat:   row.ActiveVersionAssetFormat.String,
		SizeBytes:     row.ActiveVersionSizeBytes.Int64,
		AuthorName:    conv.FromPGText[string](row.ActiveVersionAuthorName),
		State:         conv.PtrEmpty("active"),
		CreatedAt:     row.ActiveVersionCreatedAt.Time.Format(time.RFC3339),
		FirstSeenAt:   firstSeenAt,
	}
}

func buildSkillVersion(version *skillsrepo.SkillVersion) *gen.SkillVersion {
	if version == nil {
		return nil
	}

	var assetID *string
	if version.AssetID != uuid.Nil {
		assetID = conv.PtrEmpty(version.AssetID.String())
	}

	var skillBytes *int64
	if version.SkillBytes.Valid {
		skillBytes = conv.PtrEmpty(version.SkillBytes.Int64)
	}

	var firstSeenAt *string
	if version.FirstSeenAt.Valid {
		firstSeenAt = conv.PtrEmpty(version.FirstSeenAt.Time.Format(time.RFC3339))
	}

	var rejectedAt *string
	if version.RejectedAt.Valid {
		rejectedAt = conv.PtrEmpty(version.RejectedAt.Time.Format(time.RFC3339))
	}

	return &gen.SkillVersion{
		ID:                 version.ID.String(),
		SkillID:            version.SkillID.String(),
		AssetID:            assetID,
		ContentSha256:      version.ContentSha256,
		AssetFormat:        version.AssetFormat,
		SizeBytes:          version.SizeBytes,
		SkillBytes:         skillBytes,
		State:              version.State,
		CapturedByUserID:   conv.PtrEmpty(version.CapturedByUserID),
		AuthorName:         conv.FromPGText[string](version.AuthorName),
		RejectedByUserID:   conv.FromPGText[string](version.RejectedByUserID),
		RejectedReason:     conv.FromPGText[string](version.RejectedReason),
		RejectedAt:         rejectedAt,
		FirstSeenTraceID:   conv.FromPGText[string](version.FirstSeenTraceID),
		FirstSeenSessionID: conv.FromPGText[string](version.FirstSeenSessionID),
		FirstSeenAt:        firstSeenAt,
		CreatedAt:          version.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:          version.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func (s *Service) findExistingAsset(ctx context.Context, projectID uuid.UUID, sha string) (*gen.CaptureSkillAsset, error) {
	asset, err := s.repo.GetProjectAssetBySHA256(ctx, assetsrepo.GetProjectAssetBySHA256Params{
		ProjectID: projectID,
		Sha256:    sha,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("find existing skill asset: %w", err), "error loading skill asset")
	}
	if asset.Deleted {
		return nil, nil
	}
	if asset.Kind != skillAssetKind {
		return nil, oops.E(oops.CodeConflict, nil, "skill asset hash conflicts with existing non-skill asset")
	}

	assetURL, err := url.Parse(asset.Url)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid existing asset url", attr.SlogURLFull(asset.Url), attr.SlogError(err))
		return nil, nil
	}

	exists, err := s.storage.Exists(ctx, assetURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("check existing skill asset: %w", err), "error loading skill asset")
	}
	if !exists {
		if err := s.archiveStaleSkillAsset(ctx, projectID, asset.ID); err != nil {
			return nil, err
		}
		return nil, nil
	}

	return &gen.CaptureSkillAsset{
		ID:            asset.ID.String(),
		Kind:          asset.Kind,
		Sha256:        asset.Sha256,
		ContentType:   asset.ContentType,
		ContentLength: asset.ContentLength,
		CreatedAt:     asset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     asset.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) uploadAsset(ctx context.Context, projectID uuid.UUID, filename string, contentType string, contentLength int64, file *os.File) (*url.URL, error) {
	dst, uri, err := s.storage.Write(ctx, path.Join(projectID.String(), filename), contentType, contentLength)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("write to blob storage: %w", err), "error uploading skill artifact")
	}
	dstClosed := false
	defer o11y.LogDefer(ctx, s.logger, func() error {
		if dstClosed {
			return nil
		}
		if err := dst.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("close blob storage writer: %w", err)
		}
		return nil
	})

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("seek skill file: %w", err), "error uploading skill artifact")
	}

	n, err := io.Copy(dst, file)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("copy to blob storage: %w", err), "error uploading skill artifact")
	}
	if n != contentLength {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("expected %d bytes, wrote %d", contentLength, n), "error uploading skill artifact")
	}

	if err := dst.Close(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("finalize blob storage: %w", err), "error uploading skill artifact")
	}
	dstClosed = true

	return uri, nil
}
