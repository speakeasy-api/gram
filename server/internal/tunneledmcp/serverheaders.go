package tunneledmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// headerConflictMessage is returned when a header name is already in use on
// the tunnel. Names are compared case-insensitively.
const headerConflictMessage = "a header with this name already exists on this tunneled mcp server"

// headerNotFoundMessage is returned for a header id that is unknown, deleted,
// belongs to a deleted tunnel, or belongs to another project.
const headerNotFoundMessage = "tunneled mcp server header not found"

// headerWrite is a validated header write in the form it is stored.
type headerWrite struct {
	// name is the canonical destination header name.
	name string

	// valueFromRequestHeader is the canonical pass-through source, or empty.
	valueFromRequestHeader string

	// value is the static value, or nil for a pass-through header or a
	// preserved secret.
	value *string
}

// validateHeaderWrite applies the tunneled header policy to a create or
// update. preserveStoredValue is true when an update omits the value of a
// header that is already secret, which keeps the stored value.
func validateHeaderWrite(name string, value *string, valueFromRequestHeader *string, isSecret bool, preserveStoredValue bool) (headerWrite, error) {
	canonicalName, err := proxy.NormalizeHeaderName(name)
	if err != nil {
		return headerWrite{}, fmt.Errorf("name: %w", err)
	}

	hasValue := value != nil && *value != ""
	hasSource := valueFromRequestHeader != nil && *valueFromRequestHeader != ""

	var source string
	if hasSource {
		source, err = proxy.NormalizeHeaderName(*valueFromRequestHeader)
		if err != nil {
			return headerWrite{}, fmt.Errorf("value_from_request_header: %w", err)
		}
	}

	switch {
	case preserveStoredValue && !hasValue && !hasSource:
	case hasValue == hasSource:
		return headerWrite{}, fmt.Errorf("header %q must specify exactly one of value or value_from_request_header", canonicalName)
	case hasSource && isSecret:
		return headerWrite{}, fmt.Errorf("header %q: pass-through headers cannot be marked as secret", canonicalName)
	}

	staticValue := ""
	if hasValue {
		staticValue = *value
	}
	if err := proxy.CheckTunneledHeader(proxy.ConfiguredHeader{
		IsRequired:             false,
		Name:                   canonicalName,
		StaticValue:            staticValue,
		ValueFromRequestHeader: source,
	}); err != nil {
		return headerWrite{}, fmt.Errorf("header %q: %w", canonicalName, err)
	}

	write := headerWrite{name: canonicalName, valueFromRequestHeader: source, value: nil}
	if hasValue {
		write.value = &staticValue
	}
	return write, nil
}

func invalidHeaderError(ctx context.Context, logger *slog.Logger, err error) error {
	return oops.E(oops.CodeBadRequest, err, "invalid header: %s", err.Error()).LogWarn(ctx, logger)
}

func (s *Service) headers(db repo.DBTX) *Headers {
	return NewHeaders(s.logger, db, s.enc)
}

func (s *Service) ListServerHeaders(ctx context.Context, payload *gen.ListServerHeadersPayload) (*gen.ListTunneledMcpServerHeadersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.TunneledMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, logger)
	}

	if _, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{ID: serverID, ProjectID: *authCtx.ProjectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "tunneled mcp server not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server").LogError(ctx, logger)
	}

	headers, err := s.headers(s.db).ListServerHeaders(ctx, serverID, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list tunneled mcp server headers").LogError(ctx, logger)
	}

	return &gen.ListTunneledMcpServerHeadersResult{Headers: mv.BuildTunneledMcpServerHeaderListView(headers)}, nil
}

func (s *Service) GetServerHeader(ctx context.Context, payload *gen.GetServerHeaderPayload) (*types.TunneledMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid header id").LogWarn(ctx, logger)
	}

	header, err := s.headers(s.db).GetServerHeader(ctx, headerID, *authCtx.ProjectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, headerNotFoundMessage).LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server header").LogError(ctx, logger)
	}

	return mv.BuildTunneledMcpServerHeaderView(header), nil
}

func (s *Service) CreateServerHeader(ctx context.Context, payload *gen.CreateServerHeaderPayload) (*types.TunneledMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.TunneledMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, logger)
	}

	isSecret := conv.PtrValOr(payload.IsSecret, false)
	write, err := validateHeaderWrite(payload.Name, payload.Value, payload.ValueFromRequestHeader, isSecret, false)
	if err != nil {
		return nil, invalidHeaderError(ctx, logger, err)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Locking the tunnel serializes header writes with each other and with
	// deleting the tunnel, so a header cannot be created after the delete's
	// cascade has run, and the duplicate-name check below cannot race.
	server, err := txRepo.GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: serverID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "tunneled mcp server not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock tunneled mcp server").LogError(ctx, logger)
	}

	if err := s.requireUniqueHeaderName(ctx, txRepo, server.ID, write.name, uuid.Nil); err != nil {
		return nil, err
	}

	header, err := s.headers(dbtx).CreateServerHeader(ctx, repo.CreateServerHeaderParams{
		Name:                   write.name,
		Description:            conv.PtrToPGText(payload.Description),
		IsRequired:             conv.PtrValOr(payload.IsRequired, false),
		IsSecret:               isSecret,
		Value:                  conv.PtrToPGTextEmpty(write.value),
		ValueFromRequestHeader: conv.ToPGTextEmpty(write.valueFromRequestHeader),
		TunneledMcpServerID:    server.ID,
		ProjectID:              *authCtx.ProjectID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, oops.E(oops.CodeConflict, err, headerConflictMessage).LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create tunneled mcp server header").LogError(ctx, logger)
	}

	if err := s.audit.LogTunneledMcpServerHeaderCreate(ctx, dbtx, audit.LogTunneledMcpServerHeaderCreateEvent{
		OrganizationID:              authCtx.ActiveOrganizationID,
		ProjectID:                   *authCtx.ProjectID,
		Actor:                       urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:            authCtx.Email,
		ActorSlug:                   nil,
		TunneledMcpServerHeaderURN:  urn.NewTunneledMcpServerHeader(header.ID),
		TunneledMcpServerHeaderName: header.Name,
		TunneledMcpServerURN:        urn.NewTunneledMcpServer(server.ID),
		TunneledMcpServerName:       server.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tunneled mcp server header creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildTunneledMcpServerHeaderView(header), nil
}

func (s *Service) UpdateServerHeader(ctx context.Context, payload *gen.UpdateServerHeaderPayload) (*types.TunneledMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid header id").LogWarn(ctx, logger)
	}

	isSecret := conv.PtrValOr(payload.IsSecret, false)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	server, existing, err := s.lockHeader(ctx, logger, txRepo, headerID, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	// Omitting the value of a header that is already secret preserves the
	// stored value. Decided against the row as it stands under the lock.
	preserveStoredValue := isSecret && existing.IsSecret && existing.Value.Valid
	write, err := validateHeaderWrite(payload.Name, payload.Value, payload.ValueFromRequestHeader, isSecret, preserveStoredValue)
	if err != nil {
		return nil, invalidHeaderError(ctx, logger, err)
	}
	setValue := write.value != nil || write.valueFromRequestHeader != ""

	if err := s.requireUniqueHeaderName(ctx, txRepo, server.ID, write.name, existing.ID); err != nil {
		return nil, err
	}

	beforeView := mv.BuildTunneledMcpServerHeaderView(redactHeader(existing))

	header, err := s.headers(dbtx).UpdateServerHeader(ctx, repo.UpdateServerHeaderParams{
		Name:                   write.name,
		Description:            conv.PtrToPGText(payload.Description),
		IsRequired:             conv.PtrValOr(payload.IsRequired, false),
		IsSecret:               isSecret,
		SetValue:               setValue,
		Value:                  conv.PtrToPGTextEmpty(write.value),
		ValueFromRequestHeader: conv.ToPGTextEmpty(write.valueFromRequestHeader),
		ID:                     existing.ID,
		ProjectID:              *authCtx.ProjectID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, oops.E(oops.CodeConflict, err, headerConflictMessage).LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update tunneled mcp server header").LogError(ctx, logger)
	}

	afterView := mv.BuildTunneledMcpServerHeaderView(header)

	if err := s.audit.LogTunneledMcpServerHeaderUpdate(ctx, dbtx, audit.LogTunneledMcpServerHeaderUpdateEvent{
		OrganizationID:                        authCtx.ActiveOrganizationID,
		ProjectID:                             *authCtx.ProjectID,
		Actor:                                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                      authCtx.Email,
		ActorSlug:                             nil,
		TunneledMcpServerHeaderURN:            urn.NewTunneledMcpServerHeader(header.ID),
		TunneledMcpServerHeaderName:           header.Name,
		TunneledMcpServerURN:                  urn.NewTunneledMcpServer(server.ID),
		TunneledMcpServerName:                 server.Name,
		TunneledMcpServerHeaderSnapshotBefore: beforeView,
		TunneledMcpServerHeaderSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tunneled mcp server header update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) DeleteServerHeader(ctx context.Context, payload *gen.DeleteServerHeaderPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid header id").LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	server, existing, err := s.lockHeader(ctx, logger, txRepo, headerID, *authCtx.ProjectID)
	if err != nil {
		return err
	}

	deleted, err := txRepo.DeleteServerHeader(ctx, repo.DeleteServerHeaderParams{ID: existing.ID, ProjectID: *authCtx.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, headerNotFoundMessage).LogWarn(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete tunneled mcp server header").LogError(ctx, logger)
	}

	if err := s.audit.LogTunneledMcpServerHeaderDelete(ctx, dbtx, audit.LogTunneledMcpServerHeaderDeleteEvent{
		OrganizationID:              authCtx.ActiveOrganizationID,
		ProjectID:                   *authCtx.ProjectID,
		Actor:                       urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:            authCtx.Email,
		ActorSlug:                   nil,
		TunneledMcpServerHeaderURN:  urn.NewTunneledMcpServerHeader(deleted.ID),
		TunneledMcpServerHeaderName: deleted.Name,
		TunneledMcpServerURN:        urn.NewTunneledMcpServer(server.ID),
		TunneledMcpServerName:       server.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log tunneled mcp server header deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// lockHeader resolves a header to its tunnel within the project, locks the
// tunnel, and re-reads the header under that lock, so the caller decides on
// the header as it stands while no other header write or tunnel delete can
// run. Any miss is reported as not found.
func (s *Service) lockHeader(ctx context.Context, logger *slog.Logger, txRepo *repo.Queries, headerID uuid.UUID, projectID uuid.UUID) (repo.TunneledMcpServer, repo.TunneledMcpServerHeader, error) {
	notFound := func(err error) error {
		return oops.E(oops.CodeNotFound, err, headerNotFoundMessage).LogWarn(ctx, logger)
	}

	unlocked, err := txRepo.GetServerHeader(ctx, repo.GetServerHeaderParams{ID: headerID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.TunneledMcpServer{}, repo.TunneledMcpServerHeader{}, notFound(err)
		}
		return repo.TunneledMcpServer{}, repo.TunneledMcpServerHeader{}, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server header").LogError(ctx, logger)
	}

	server, err := txRepo.GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{ID: unlocked.TunneledMcpServerID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.TunneledMcpServer{}, repo.TunneledMcpServerHeader{}, notFound(err)
		}
		return repo.TunneledMcpServer{}, repo.TunneledMcpServerHeader{}, oops.E(oops.CodeUnexpected, err, "lock tunneled mcp server").LogError(ctx, logger)
	}

	header, err := txRepo.GetServerHeader(ctx, repo.GetServerHeaderParams{ID: headerID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.TunneledMcpServer{}, repo.TunneledMcpServerHeader{}, notFound(err)
		}
		return repo.TunneledMcpServer{}, repo.TunneledMcpServerHeader{}, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server header").LogError(ctx, logger)
	}

	return server, header, nil
}

// requireUniqueHeaderName rejects a name already used, in any letter case, by
// another live header on the tunnel. Callers hold the tunnel lock.
func (s *Service) requireUniqueHeaderName(ctx context.Context, txRepo *repo.Queries, serverID uuid.UUID, name string, excludeID uuid.UUID) error {
	_, err := txRepo.FindLiveServerHeaderByName(ctx, repo.FindLiveServerHeaderByNameParams{
		TunneledMcpServerID: serverID,
		Name:                name,
		ExcludeID:           excludeID,
	})
	switch {
	case err == nil:
		return oops.E(oops.CodeConflict, nil, headerConflictMessage).LogWarn(ctx, s.logger)
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	default:
		return oops.E(oops.CodeUnexpected, err, "check tunneled mcp server header name").LogError(ctx, s.logger)
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}
