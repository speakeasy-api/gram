// Package platformaiscantargets is the platform-administrator management
// service for the Shadow AI scan target catalog. Reads require the users.admin
// entitlement; writes require a fresh Gram session. The catalog has no
// organization, so changes are recorded on ai_scan_catalog_revisions rather
// than the org-scoped audit log.
package platformaiscantargets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/platform_ai_scan_targets/server"
	gen "github.com/speakeasy-api/gram/server/gen/platform_ai_scan_targets"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	gramauth "github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const maxRequestBodyBytes = 1 << 20

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *gramauth.Auth
	sessions gramauth.PlatformAdminEntitlementReader
	catalog  *aitargets.Catalog
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	catalog *aitargets.Catalog,
) *Service {
	logger = logger.With(attr.SlogComponent("platformaiscantargets"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/platformaiscantargets"),
		logger:   logger,
		db:       db,
		auth:     gramauth.New(logger, db, sessionManager, authzEngine),
		sessions: sessionManager,
		catalog:  catalog,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, requestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func requestDecoder(r *http.Request) goahttp.Decoder {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(nil, r.Body, maxRequestBodyBytes)
	}
	return goahttp.RequestDecoder(r)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) List(ctx context.Context, _ *gen.ListPayload) (*gen.ListAiScanTargetsResult, error) {
	if _, _, err := gramauth.RequirePlatformAdmin(ctx, s.logger); err != nil {
		return nil, err
	}

	if err := aitargets.SeedDefaults(ctx, s.db); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error preparing ai scan catalog").LogError(ctx, s.logger)
	}
	records, version, err := s.catalog.ListRecords(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing ai scan targets").LogError(ctx, s.logger)
	}
	served := make([]aitargets.Target, 0, len(records))
	for _, record := range records {
		if record.Enabled {
			served = append(served, record.Target)
		}
	}
	return mv.BuildAiScanTargetListView(version, aitargets.NewSnapshot(version, served).ETag, records), nil
}

func (s *Service) Upsert(ctx context.Context, payload *gen.UpsertPayload) (*gen.AiScanTargetMutationResult, error) {
	actor, logger, err := s.authorizeWrite(ctx)
	if err != nil {
		return nil, err
	}
	if err := aitargets.SeedDefaults(ctx, s.db); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error preparing ai scan catalog").LogError(ctx, logger)
	}

	target := aitargets.Target{
		ID:          strings.TrimSpace(payload.ID),
		DisplayName: strings.TrimSpace(payload.DisplayName),
		Category:    aitargets.Category(strings.TrimSpace(payload.Category)),
		Signatures:  signaturesFromPayload(payload.Signatures),
		VersionHint: nil,
		Enabled:     payload.Enabled,
	}
	if key := strings.TrimSpace(conv.PtrValOr(payload.VersionPlistKey, "")); key != "" {
		target.VersionHint = &aitargets.VersionHint{PlistKey: key}
	}
	if err := aitargets.ValidateTarget(target); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%v", err)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting catalog update").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireAIScanCatalogLock(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking ai scan catalog").LogError(ctx, logger)
	}

	var before *aitargets.Target
	existing, err := queries.GetAIScanTargetForUpdate(ctx, target.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error reading ai scan target").LogError(ctx, logger)
	case !existing.Deleted:
		current := aitargets.RecordFromRow(existing).Target
		before = &current
	}

	row, err := queries.UpsertAIScanTarget(ctx, aitargets.UpsertParams(target))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving ai scan target").LogError(ctx, logger)
	}
	if err := validateServedSet(ctx, queries); err != nil {
		return nil, err
	}
	record := aitargets.RecordFromRow(row)
	version, err := aitargets.RecordRevision(ctx, queries, aitargets.Revision{
		TargetID: record.ID,
		Action:   aitargets.ActionUpsert,
		Actor:    actor,
		Reason:   strings.TrimSpace(conv.PtrValOr(payload.Reason, "")),
		Before:   before,
		After:    &record.Target,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording catalog revision").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing catalog update").LogError(ctx, logger)
	}
	s.catalog.Invalidate()

	return &gen.AiScanTargetMutationResult{ListVersion: int(version), Target: mv.BuildAiScanTargetView(record)}, nil
}

func (s *Service) SetEnabled(ctx context.Context, payload *gen.SetEnabledPayload) (*gen.AiScanTargetMutationResult, error) {
	actor, logger, err := s.authorizeWrite(ctx)
	if err != nil {
		return nil, err
	}
	if err := aitargets.SeedDefaults(ctx, s.db); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error preparing ai scan catalog").LogError(ctx, logger)
	}
	id := strings.TrimSpace(payload.ID)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting catalog update").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireAIScanCatalogLock(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking ai scan catalog").LogError(ctx, logger)
	}

	before, err := lockLiveTarget(ctx, queries, id)
	if err != nil {
		return nil, mapLookupError(ctx, logger, err, id)
	}
	row, err := queries.SetAIScanTargetEnabled(ctx, repo.SetAIScanTargetEnabledParams{Enabled: payload.Enabled, ID: id})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating ai scan target").LogError(ctx, logger)
	}
	if err := validateServedSet(ctx, queries); err != nil {
		return nil, err
	}
	record := aitargets.RecordFromRow(row)
	action := aitargets.ActionDisable
	if payload.Enabled {
		action = aitargets.ActionEnable
	}
	version, err := aitargets.RecordRevision(ctx, queries, aitargets.Revision{
		TargetID: record.ID,
		Action:   action,
		Actor:    actor,
		Reason:   strings.TrimSpace(conv.PtrValOr(payload.Reason, "")),
		Before:   &before.Target,
		After:    &record.Target,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording catalog revision").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing catalog update").LogError(ctx, logger)
	}
	s.catalog.Invalidate()

	return &gen.AiScanTargetMutationResult{ListVersion: int(version), Target: mv.BuildAiScanTargetView(record)}, nil
}

func (s *Service) Delete(ctx context.Context, payload *gen.DeletePayload) (*gen.DeleteAiScanTargetResult, error) {
	actor, logger, err := s.authorizeWrite(ctx)
	if err != nil {
		return nil, err
	}
	if err := aitargets.SeedDefaults(ctx, s.db); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error preparing ai scan catalog").LogError(ctx, logger)
	}
	id := strings.TrimSpace(payload.ID)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting catalog update").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)
	if err := queries.AcquireAIScanCatalogLock(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking ai scan catalog").LogError(ctx, logger)
	}

	before, err := lockLiveTarget(ctx, queries, id)
	if err != nil {
		return nil, mapLookupError(ctx, logger, err, id)
	}
	if _, err := queries.SoftDeleteAIScanTarget(ctx, id); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error deleting ai scan target").LogError(ctx, logger)
	}
	version, err := aitargets.RecordRevision(ctx, queries, aitargets.Revision{
		TargetID: id,
		Action:   aitargets.ActionDelete,
		Actor:    actor,
		Reason:   strings.TrimSpace(conv.PtrValOr(payload.Reason, "")),
		Before:   &before.Target,
		After:    nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording catalog revision").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing catalog update").LogError(ctx, logger)
	}
	s.catalog.Invalidate()

	return &gen.DeleteAiScanTargetResult{ListVersion: int(version)}, nil
}

func (s *Service) ListRevisions(ctx context.Context, payload *gen.ListRevisionsPayload) (*gen.ListAiScanCatalogRevisionsResult, error) {
	if _, _, err := gramauth.RequirePlatformAdmin(ctx, s.logger); err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListAIScanCatalogRevisions(ctx, int32(payload.Limit)) // #nosec G115 -- the Goa payload caps limit at 200.
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing ai scan catalog revisions").LogError(ctx, s.logger)
	}
	revisions := make([]*gen.AiScanCatalogRevision, 0, len(rows))
	for _, row := range rows {
		revisions = append(revisions, mv.BuildAiScanCatalogRevisionView(row))
	}
	return &gen.ListAiScanCatalogRevisionsResult{Revisions: revisions}, nil
}

// authorizeWrite gates a mutation on a fresh platform-admin session.
func (s *Service) authorizeWrite(ctx context.Context) (aitargets.Actor, *slog.Logger, error) {
	authCtx, logger, err := gramauth.RequireFreshPlatformAdminSession(ctx, s.logger, s.sessions)
	if err != nil {
		return aitargets.Actor{UserID: "", Email: ""}, s.logger, fmt.Errorf("authorize ai scan catalog write: %w", err)
	}
	return aitargets.Actor{
		UserID: authCtx.UserID,
		Email:  strings.TrimSpace(conv.PtrValOr(authCtx.Email, "")),
	}, logger, nil
}

// validateServedSet rejects a write that would make the enabled set exceed
// what agents accept. Runs inside the write transaction, under the catalog
// lock, so the count and size caps hold across concurrent writers.
func validateServedSet(ctx context.Context, queries *repo.Queries) error {
	rows, err := queries.ListEnabledAIScanTargets(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error reading the served ai scan targets")
	}
	served := make([]aitargets.Target, 0, len(rows))
	for _, row := range rows {
		served = append(served, aitargets.RecordFromRow(row).Target)
	}
	if err := aitargets.ValidateServed(served); err != nil {
		return oops.E(oops.CodeBadRequest, err, "%v", err)
	}
	return nil
}

// lockLiveTarget locks the row for id, treating a missing or tombstoned row
// as aitargets.ErrNotFound.
func lockLiveTarget(ctx context.Context, queries *repo.Queries, id string) (aitargets.Record, error) {
	row, err := queries.GetAIScanTargetForUpdate(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return aitargets.Record{}, aitargets.ErrNotFound
	case err != nil:
		return aitargets.Record{}, fmt.Errorf("lock ai scan target %q: %w", id, err)
	case row.Deleted:
		return aitargets.Record{}, aitargets.ErrNotFound
	}
	return aitargets.RecordFromRow(row), nil
}

func mapLookupError(ctx context.Context, logger *slog.Logger, err error, id string) error {
	if errors.Is(err, aitargets.ErrNotFound) {
		return oops.E(oops.CodeNotFound, err, "ai scan target %q not found", id)
	}
	return oops.E(oops.CodeUnexpected, err, "error reading ai scan target").LogError(ctx, logger)
}

func signaturesFromPayload(signatures *gen.AiScanTargetSignatures) aitargets.Signatures {
	if signatures == nil {
		return aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{}, ConfigDirs: []string{}, ProcessNames: []string{}}
	}
	return aitargets.Signatures{
		BundleIDs:    trimAll(signatures.BundleIds),
		Binaries:     trimAll(signatures.Binaries),
		ConfigDirs:   trimAll(signatures.ConfigDirs),
		ProcessNames: trimAll(signatures.ProcessNames),
	}
}

// trimAll trims entries and drops blank ones.
func trimAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
