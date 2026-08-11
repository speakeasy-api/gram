package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ListClients lists the clients registered with an issuer in the caller's
// organization, each with its MCP server attachment count.
func (s *Service) ListClients(ctx context.Context, payload *orgclientsgen.ListClientsPayload) (*orgclientsgen.ListOrganizationRemoteSessionClientsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.IssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}

	rows, err := repo.New(s.db).ListOrganizationRemoteSessionClientsByIssuerID(ctx, repo.ListOrganizationRemoteSessionClientsByIssuerIDParams{
		RemoteSessionIssuerID: issuerID,
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization admin remote session clients").LogError(ctx, logger)
	}

	items := make([]*orgclientsgen.OrganizationRemoteSessionClient, 0, len(rows))
	for _, row := range rows {
		clientView, err := mv.BuildRemoteSessionClientView(row.RemoteSessionClient, row.UserSessionIssuerIds)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
		}
		items = append(items, &orgclientsgen.OrganizationRemoteSessionClient{
			Client:             clientView,
			McpServerCount:     int(row.McpServerCount),
			ActiveSessionCount: int(row.ActiveSessionCount),
		})
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].RemoteSessionClient.ID.String()
		nextCursor = &c
	}

	return &orgclientsgen.ListOrganizationRemoteSessionClientsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetClient resolves a client in the caller's organization by id.
func (s *Service) GetClient(ctx context.Context, payload *orgclientsgen.GetClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	client, err := repo.New(s.db).GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	view, err := mv.BuildRemoteSessionClientView(client.RemoteSessionClient, client.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
	}
	return view, nil
}

// GetClientDeletePreflight returns the authoritative impact of deleting a
// client: session count and the names of MCP servers it is attached to.
func (s *Service) GetClientDeletePreflight(ctx context.Context, payload *orgclientsgen.GetClientDeletePreflightPayload) (*orgclientsgen.OrganizationClientDeletePreflight, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	if _, err := r.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	sessionCount, err := r.CountActiveRemoteSessionsByClientID(ctx, clientID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count remote sessions").LogError(ctx, logger)
	}

	mcpRows, err := r.ListOrganizationMcpServersForClient(ctx, clientID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers for client").LogError(ctx, logger)
	}

	names := make([]string, 0, len(mcpRows))
	for _, row := range mcpRows {
		names = append(names, orgDisplayName(conv.FromPGText[string](row.Name), row.Url))
	}

	return &orgclientsgen.OrganizationClientDeletePreflight{
		SessionCount:   int(sessionCount),
		McpServerNames: names,
	}, nil
}

// ListClientMcpServers lists the MCP servers a client is attached to in the
// caller's organization.
func (s *Service) ListClientMcpServers(ctx context.Context, payload *orgclientsgen.ListClientMcpServersPayload) (*orgclientsgen.ListOrganizationMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	// Establish org ownership of the client before resolving its MCP servers.
	if _, err := r.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	rows, err := r.ListOrganizationMcpServersForClient(ctx, clientID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers for client").LogError(ctx, logger)
	}

	items := make([]*orgclientsgen.OrganizationMcpServer, 0, len(rows))
	for _, row := range rows {
		items = append(items, &orgclientsgen.OrganizationMcpServer{
			ID:          row.ID.String(),
			ProjectID:   row.ProjectID.String(),
			ProjectSlug: conv.PtrEmpty(row.ProjectSlug),
			Name:        conv.FromPGText[string](row.Name),
			Slug:        conv.FromPGText[string](row.Slug),
			URL:         conv.PtrEmpty(row.Url),
		})
	}

	return &orgclientsgen.ListOrganizationMcpServersResult{Items: items}, nil
}

// CreateClient registers a standalone remote_session_client under an existing
// issuer in the caller's organization, with no user_session_issuer attachments.
// The client inherits a project-specific issuer's project; under an
// organization-level issuer it is created organization-level (NULL project_id,
// attachable by every project in the org) unless the caller names a project to
// downscope it to. Gated on org:admin like the other org-admin client writes.
// Standalone clients are intentionally not deduplicated by client_id, matching
// the project-scoped create path.
func (s *Service) CreateClient(ctx context.Context, payload *orgclientsgen.CreateClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	clientID := strings.TrimSpace(payload.ClientID)
	if clientID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id is required").LogError(ctx, logger)
	}

	// Encrypt a supplied client secret before it touches the database; an absent
	// secret leaves the stored ciphertext NULL.
	var secretCiphertext pgtype.Text
	if payload.ClientSecret != nil && *payload.ClientSecret != "" {
		ciphertext, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").LogError(ctx, logger)
		}
		secretCiphertext = conv.ToPGText(ciphertext)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Serialize against migrateIssuer and against DeleteGlobalIssuer before
	// reading the issuer. Without it a concurrent migration could soft-delete
	// this issuer between the read and the insert, leaving a client bound to a
	// tombstoned issuer, and a concurrent global-issuer delete could count zero
	// clients and delete the issuer out from under this insert. The project-level
	// create paths have always taken this lock; these org-admin paths did not,
	// which was a pre-existing gap against migrateIssuer independent of platform
	// issuers. See the full rationale in validateNewClientIssuers.
	if err := txRepo.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock remote session issuer for client binding").LogError(ctx, logger)
	}

	// Reject an issuer that isn't in the caller's organization so a client can't
	// be registered against another tenant's issuer. Platform issuers resolve
	// too: the client row is owned by this organization, only the issuer is
	// shared.
	issuer, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  true,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	clientProjectID, err := s.resolveOrganizationClientProject(ctx, dbtx, logger, issuer, payload.ProjectID, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               clientProjectID,
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuerID,
		ClientID:                clientID,
		ClientSecretEncrypted:   secretCiphertext,
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now().UTC()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		LegacyCallbackUrl:       false,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create organization admin remote session client").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientCreate(ctx, dbtx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(created.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(created.ID),
		ClientID:               created.ClientID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session client creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Standalone client: no user_session_issuer attachments.
	view, err := mv.BuildRemoteSessionClientView(created, nil)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	return view, nil
}

// resolveOrganizationClientProject determines the owning project for an
// org-admin-created standalone remote_session_client, mirroring how createIssuer
// scopes an issuer: a supplied project_id (validated to belong to the org)
// scopes the client to that project, an omitted project_id inherits a
// project-specific issuer's own project, and an omitted project_id under an
// organization-level issuer yields an invalid NullUUID that persists as a NULL
// project_id — an organization-level client every project in the org can attach.
// For a project-specific issuer a supplied project_id must match the issuer's
// own project so the client stays reachable from it. An organization-level
// client therefore arises from an issuer that carries no project of its own:
// an organization-level issuer, or a platform issuer from the shared catalog
// (whose project_id is likewise NULL). Must run inside the create transaction.
// Shared by CreateClient and CreateCimdClient.
func (s *Service) resolveOrganizationClientProject(ctx context.Context, dbtx pgx.Tx, logger *slog.Logger, issuer repo.RemoteSessionIssuer, payloadProjectID *string, organizationID string) (uuid.NullUUID, error) {
	clientProjectID := issuer.ProjectID
	if payloadProjectID != nil && strings.TrimSpace(*payloadProjectID) != "" {
		pid, err := uuid.Parse(*payloadProjectID)
		if err != nil {
			return uuid.NullUUID{}, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, logger)
		}
		if issuer.ProjectID.Valid && issuer.ProjectID.UUID != pid {
			return uuid.NullUUID{}, oops.E(oops.CodeBadRequest, nil, "project_id must match the issuer's project for a project-specific issuer").LogError(ctx, logger)
		}
		if _, err := projectsrepo.New(dbtx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
			ID:             pid,
			OrganizationID: organizationID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return uuid.NullUUID{}, oops.E(oops.CodeBadRequest, err, "project not found in organization").LogError(ctx, logger)
			}
			return uuid.NullUUID{}, oops.E(oops.CodeUnexpected, err, "validate project").LogError(ctx, logger)
		}
		clientProjectID = conv.ToNullUUID(pid)
	}
	return clientProjectID, nil
}

// CreateCimdClient registers a standalone remote_session_client in Client ID
// Metadata Document (CIMD) mode under an issuer in the caller's organization.
// Like CreateClient the project is resolved from the issuer or the
// caller-supplied project_id (and may be organization-level under an
// organization-level issuer), but the caller supplies no
// credentials: Gram generates the client_id and serves the metadata document,
// and the issuer must advertise CIMD support.
func (s *Service) CreateCimdClient(ctx context.Context, payload *orgclientsgen.CreateCimdClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Serialize against migrateIssuer and DeleteGlobalIssuer before reading the
	// issuer; see the matching lock in CreateClient for the full rationale.
	if err := txRepo.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock remote session issuer for client binding").LogError(ctx, logger)
	}

	// Reject an issuer that isn't in the caller's organization so a client can't
	// be registered against another tenant's issuer. Platform issuers resolve
	// too: the client row is owned by this organization, only the issuer is
	// shared.
	issuer, err := txRepo.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:  true,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session issuer").LogError(ctx, logger)
	}

	// Pre-flight against the issuer's discovered capabilities so an unsupported
	// pairing fails at create time, not at the first outbound call.
	if err := preflightCIMDIssuer(issuer); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "issuer does not support client id metadata documents").LogError(ctx, logger)
	}

	clientProjectID, err := s.resolveOrganizationClientProject(ctx, dbtx, logger, issuer, payload.ProjectID, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	// Generate the id up front so the document URL (which embeds it) is the
	// client_id on a single INSERT.
	clientID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate client id").LogError(ctx, logger)
	}

	created, err := txRepo.CreateRemoteSessionClientCIMD(ctx, repo.CreateRemoteSessionClientCIMDParams{
		ID:                    clientID,
		ProjectID:             clientProjectID,
		OrganizationID:        conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID: issuerID,
		ClientIDMetadataUri:   ClientMetadataDocumentURL(s.serverURL, clientID),
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
		Scope:                 payload.Scope,
		Audience:              conv.PtrToPGText(payload.Audience),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create organization admin remote session client").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientCreate(ctx, dbtx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(created.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(created.ID),
		ClientID:               created.ClientID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session client creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Standalone client: no user_session_issuer attachments.
	view, err := mv.BuildRemoteSessionClientView(created, nil)
	if err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "build remote session client view").LogError(ctx, logger)
	}

	return view, nil
}

// UpdateClient patches a client's non-secret fields in the caller's
// organization.
func (s *Service) UpdateClient(ctx context.Context, payload *orgclientsgen.UpdateClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	// Encrypt a rotated client secret before it touches the database; an absent
	// secret leaves the stored ciphertext untouched (narg NULL → COALESCE keeps).
	var clientSecretEncrypted pgtype.Text
	if payload.ClientSecret != nil {
		if strings.TrimSpace(*payload.ClientSecret) == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "client_secret cannot be empty").LogError(ctx, logger)
		}
		ciphertext, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").LogError(ctx, logger)
		}
		clientSecretEncrypted = conv.ToPGText(ciphertext)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}

	// Issuer attachments are managed via the join table, so an org-admin update
	// never changes them; the same set frames both the before and after views.
	beforeView, err := mv.BuildRemoteSessionClientView(existing.RemoteSessionClient, existing.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
	}

	updated, err := txRepo.UpdateOrganizationRemoteSessionClient(ctx, repo.UpdateOrganizationRemoteSessionClientParams{
		ClientSecretEncrypted:   clientSecretEncrypted,
		TokenEndpointAuthMethod: conv.PtrToPGText(payload.TokenEndpointAuthMethod),
		Scope:                   payload.Scope,
		Audience:                conv.PtrToPGText(payload.Audience),
		ID:                      clientID,
		OrganizationID:          conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update organization admin remote session client").LogError(ctx, logger)
	}

	afterView, err := mv.BuildRemoteSessionClientView(updated, existing.UserSessionIssuerIds)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build remote session client view").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientUpdate(ctx, dbtx, audit.LogRemoteSessionClientUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(updated.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(updated.ID),
		ClientID:               updated.ClientID,
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log organization admin remote session client update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

// DeleteClient soft-deletes a client in the caller's organization and
// cascades the sessions minted against it.
func (s *Service) DeleteClient(ctx context.Context, payload *orgclientsgen.DeleteClientPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteOrganizationRemoteSessionClient(ctx, repo.DeleteOrganizationRemoteSessionClientParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete organization admin remote session client").LogError(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft-delete dependent remote sessions").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientDelete(ctx, dbtx, audit.LogRemoteSessionClientDeleteEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(deleted.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(deleted.ID),
		ClientID:               deleted.ClientID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization admin remote session client deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// RemoveClientFromMcpServer detaches a client from an MCP server by clearing
// the MCP server's user_session_issuer link, scoped to the caller's organization.
func (s *Service) RemoveClientFromMcpServer(ctx context.Context, payload *orgclientsgen.RemoveClientFromMcpServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	clientID, err := uuid.Parse(payload.ClientID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").LogError(ctx, logger)
	}
	mcpServerID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp_server id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Establish org ownership of the client before detaching the MCP server.
	clientRow, err := txRepo.GetOrganizationRemoteSessionClientByID(ctx, repo.GetOrganizationRemoteSessionClientByIDParams{
		ID:             clientID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "remote session client not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get organization admin remote session client").LogError(ctx, logger)
	}
	client := clientRow.RemoteSessionClient

	// Resolve the MCP server to find the user_session_issuer it uses (the binding
	// to remove) and its name (for the audit event). Scoped to the caller's org
	// (via the server's project) so a cross-org id resolves to NotFound rather
	// than reading a foreign-tenant row.
	server, err := mcpserversrepo.New(dbtx).GetMCPServerByIDAndOrganizationID(ctx, mcpserversrepo.GetMCPServerByIDAndOrganizationIDParams{
		ID:             mcpServerID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}
	if !server.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "mcp server is not attached to this client").LogError(ctx, logger)
	}

	affected, err := txRepo.DetachRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachRemoteSessionClientFromUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   server.UserSessionIssuerID.UUID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach remote session client from user session issuer").LogError(ctx, logger)
	}
	if affected == 0 {
		return oops.E(oops.CodeNotFound, nil, "mcp server is not attached to this client").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientDetachMcpServer(ctx, dbtx, audit.LogRemoteSessionClientDetachMcpServerEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              orgProjectID(client.ProjectID),
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
		ClientID:               client.ClientID,
		McpServerURN:           urn.NewMcpServer(server.ID),
		McpServerName:          conv.FromPGTextOrEmpty[string](server.Name),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log organization remote session client detach").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}
