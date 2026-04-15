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

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/gen/http/skills/server"
	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

const (
	maxCaptureArtifactMiB   = 10
	maxCaptureArtifactBytes = maxCaptureArtifactMiB * 1024 * 1024
	skillAssetKind          = "skill"
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
	access     *access.Manager
	db         *pgxpool.Pool
	storage    assets.BlobStore
	repo       *assetsrepo.Queries
	skillsRepo *skillsrepo.Queries
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionsMgr *sessions.Manager,
	storage assets.BlobStore,
	accessManager *access.Manager,
) *Service {
	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/skills"),
		logger:     logger.With(attr.SlogComponent("skills")),
		auth:       auth.New(logger, db, sessionsMgr, accessManager),
		access:     accessManager,
		db:         db,
		storage:    storage,
		repo:       assetsrepo.New(db),
		skillsRepo: skillsrepo.New(db),
	}
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

func (s *Service) Get(ctx context.Context, payload *gen.GetPayload) (*gen.Skill, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
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

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
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

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
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

	if err := s.access.Require(ctx, access.Check{Scope: access.ScopeBuildWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	mode := captureModeFromSettings(payload.Enabled, payload.CaptureProjectSkills, payload.CaptureUserSkills)
	if _, err := s.skillsRepo.UpsertProjectCapturePolicyOverride(ctx, skillsrepo.UpsertProjectCapturePolicyOverrideParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Mode:           string(mode),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("set capture policy override: %w", err), "error saving capture policy")
	}

	settings, err := s.getCaptureSettings(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	return settings, nil
}

func (s *Service) Capture(ctx context.Context, payload *gen.CaptureSkillForm, reader io.ReadCloser) (*gen.CaptureSkillResult, error) {
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return reader.Close()
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	effectiveMode, err := s.getEffectiveCaptureMode(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}
	if !captureModeAllowsScope(effectiveMode, payload.Scope) {
		if effectiveMode == CaptureModeDisabled {
			return nil, oops.E(oops.CodeForbidden, nil, "skill capture is disabled")
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
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

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
			if existing != nil {
				return &gen.CaptureSkillResult{Asset: existing}, nil
			}
			return nil, oops.E(oops.CodeConflict, nil, "skill asset already exists with incompatible metadata")
		}
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("create skill asset: %w", err), "error saving skill artifact")
	}
	if asset.Kind != skillAssetKind {
		return nil, oops.E(oops.CodeConflict, nil, "skill asset hash conflicts with existing non-skill asset")
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to save skill artifact").Log(ctx, s.logger)
	}

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
		CreatedAt:     row.ActiveVersionCreatedAt.Time.Format(time.RFC3339),
		FirstSeenAt:   firstSeenAt,
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
