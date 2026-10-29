package authclients

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// SourceAdmin marks a client or association an administrator created.
	// Its headers and associations are authoritative, unlike backfilled copies
	// of a server's existing settings.
	SourceAdmin = "admin"

	// SourceBackfill marks a client or association created from a server's
	// existing settings.
	SourceBackfill = "backfill"

	// maxHeadersPerClient bounds the headers one client sends. Upstream
	// credentials need one or two; the bound keeps one request from writing an
	// unbounded number of rows.
	maxHeadersPerClient = 32

	// maxServersPerCreate bounds the servers a create offers the client on in
	// one request, for the same reason.
	maxServersPerCreate = 100

	// registrationOwnerSubject is a registration through which each caller
	// connects their own account.
	registrationOwnerSubject = "subject"

	// registrationOwnerSelf is a registration that obtains its own token with
	// client_credentials.
	registrationOwnerSelf = "self"
)

func (s *Service) CreateAuthClient(ctx context.Context, payload *gen.CreateAuthClientPayload) (*types.AuthClient, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "an auth client needs a name").LogWarn(ctx, logger)
	}
	projectID, err := parseOptionalUUID(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogWarn(ctx, logger)
	}
	registrationID, err := parseOptionalUUID(payload.RemoteSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote session client id").LogWarn(ctx, logger)
	}
	if len(payload.McpServerIds) > maxServersPerCreate {
		return nil, oops.E(oops.CodeBadRequest, nil, "a client can be offered on at most %d servers when it is created", maxServersPerCreate).LogWarn(ctx, logger)
	}
	serverIDs := make([]uuid.UUID, 0, len(payload.McpServerIds))
	seenServers := make(map[uuid.UUID]bool, len(payload.McpServerIds))
	for _, raw := range payload.McpServerIds {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogWarn(ctx, logger)
		}
		if !seenServers[id] {
			seenServers[id] = true
			serverIDs = append(serverIDs, id)
		}
	}
	// Concurrent creates lock the same servers in one order, so they wait for
	// each other rather than deadlock.
	slices.SortFunc(serverIDs, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })

	var wantOwner string
	switch payload.ClientType {
	case authz.AuthClientTypeRemoteSession:
		wantOwner = registrationOwnerSubject
	case authz.AuthClientTypeServiceAccount:
		wantOwner = registrationOwnerSelf
	case authz.AuthClientTypeStaticCredential, authz.AuthClientTypePassThrough:
		if registrationID.Valid {
			return nil, oops.E(oops.CodeBadRequest, nil, "a %s client authenticates with its own headers and takes no OAuth registration", payload.ClientType).LogWarn(ctx, logger)
		}
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "auth clients of type %q cannot be created", payload.ClientType).LogWarn(ctx, logger)
	}
	if wantOwner != "" && !registrationID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "a %s client needs the OAuth registration it authenticates through", payload.ClientType).LogWarn(ctx, logger)
	}

	headers, err := validateClientHeaders(payload.ClientType, payload.Headers, nil)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)

	if projectID.Valid {
		if _, err := queries.GetOrganizationProject(ctx, repo.GetOrganizationProjectParams{ID: projectID.UUID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "project not found").LogWarn(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get project").LogError(ctx, logger)
		}
	}

	attachmentScope := pgtype.Text{String: "", Valid: false}
	if registrationID.Valid {
		registration, err := queries.GetAuthClientRegistration(ctx, repo.GetAuthClientRegistrationParams{
			ID:             registrationID.UUID,
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      projectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "OAuth registration not found in the client's project or organization").LogWarn(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get oauth registration").LogError(ctx, logger)
		}
		if registration.CredentialOwner != wantOwner {
			return nil, oops.E(oops.CodeBadRequest, nil, "%s", registrationOwnerMismatch(payload.ClientType)).LogWarn(ctx, logger)
		}
		if payload.ClientType == authz.AuthClientTypeServiceAccount && !registration.ProjectID.Valid && !registration.OrganizationID.Valid {
			return nil, oops.E(oops.CodeBadRequest, nil, "a service account cannot use a platform-wide OAuth registration; register the service account in your organization").LogWarn(ctx, logger)
		}
		attachmentScope = registration.AttachmentScope
	}

	created, err := queries.CreateAuthClient(ctx, repo.CreateAuthClientParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             projectID,
		ClientType:            payload.ClientType,
		Name:                  name,
		CreatedByUserID:       conv.ToPGTextEmpty(authCtx.UserID),
		RemoteSessionClientID: registrationID,
		ClientAttachmentScope: attachmentScope,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create auth client").LogError(ctx, logger)
	}

	if err := s.writeHeaders(ctx, queries, created, headers); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "write auth client headers").LogError(ctx, logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	attached := make([]repo.LockAttachableMCPServerRow, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		server, err := attachTx(ctx, queries, authCtx.ActiveOrganizationID, created, serverID)
		if err != nil {
			return nil, attachErrorResponse(ctx, logger, err)
		}
		attached = append(attached, server)
	}

	view, err := s.view(ctx, queries, authCtx.ActiveOrganizationID, created, logger)
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogAuthClientCreate(ctx, dbtx, audit.LogAuthClientUpdateEvent{
		OrganizationID:           authCtx.ActiveOrganizationID,
		ProjectID:                created.ProjectID.UUID,
		Actor:                    actor,
		ActorDisplayName:         authCtx.Email,
		ActorSlug:                nil,
		AuthClientURN:            urn.NewAuthClient(created.ID),
		AuthClientName:           created.Name,
		AuthClientSnapshotBefore: nil,
		AuthClientSnapshotAfter:  view,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log auth client create").LogError(ctx, logger)
	}
	for _, server := range attached {
		if err := logAttach(ctx, dbtx, s.audit, authCtx.ActiveOrganizationID, actor, authCtx.Email, created, server); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log auth client attach").LogError(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return view, nil
}

func (s *Service) AttachAuthClientToMcpServer(ctx context.Context, payload *gen.AttachAuthClientToMcpServerPayload) (*types.AuthClient, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid auth client id").LogWarn(ctx, logger)
	}
	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogWarn(ctx, logger)
	}

	view, err := AttachToServer(ctx, s.db, s.audit, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authCtx.Email, clientID, serverID)
	if err != nil {
		return nil, attachErrorResponse(ctx, logger, err)
	}
	return view, nil
}

func (s *Service) SetAuthClientHeaders(ctx context.Context, payload *gen.SetAuthClientHeadersPayload) (*types.AuthClient, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid auth client id").LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	client, err := queries.LockAuthClient(ctx, repo.LockAuthClientParams{ID: clientID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "auth client not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock auth client").LogError(ctx, logger)
	}
	if client.Source != SourceAdmin {
		return nil, oops.E(oops.CodeConflict, nil, "this client was created from a server's existing settings and follows them; change its headers in that server's settings").LogWarn(ctx, logger)
	}

	stored, err := queries.ListAuthClientStoredHeaders(ctx, repo.ListAuthClientStoredHeadersParams{AuthClientID: client.ID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list auth client headers").LogError(ctx, logger)
	}
	storedValues := make(map[string]storedHeaderValue, len(stored))
	for _, h := range stored {
		if h.Value.Valid {
			storedValues[headerKey(h.Name)] = storedHeaderValue{value: h.Value, isSecret: h.IsSecret}
		}
	}

	headers, err := validateClientHeaders(client.ClientType, payload.Headers, storedValues)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogWarn(ctx, logger)
	}

	before, err := s.view(ctx, queries, authCtx.ActiveOrganizationID, client, logger)
	if err != nil {
		return nil, err
	}

	if err := queries.DeleteAuthClientHeaders(ctx, repo.DeleteAuthClientHeadersParams{AuthClientID: client.ID, OrganizationID: authCtx.ActiveOrganizationID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete auth client headers").LogError(ctx, logger)
	}
	if err := s.writeHeaders(ctx, queries, client, headers); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "write auth client headers").LogError(ctx, logger)
	}

	after, err := s.view(ctx, queries, authCtx.ActiveOrganizationID, client, logger)
	if err != nil {
		return nil, err
	}

	if err := s.audit.LogAuthClientUpdateHeaders(ctx, dbtx, audit.LogAuthClientUpdateEvent{
		OrganizationID:           authCtx.ActiveOrganizationID,
		ProjectID:                client.ProjectID.UUID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:         authCtx.Email,
		ActorSlug:                nil,
		AuthClientURN:            urn.NewAuthClient(client.ID),
		AuthClientName:           client.Name,
		AuthClientSnapshotBefore: before,
		AuthClientSnapshotAfter:  after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log auth client headers update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return after, nil
}

// AttachRefusedError is why an auth client cannot be offered on a server. Its
// message is shown to the administrator as is.
type AttachRefusedError struct {
	// Code classifies the refusal.
	Code AttachRefusalKind

	// Message explains the refusal and how to fix it.
	Message string
}

func (e *AttachRefusedError) Error() string { return e.Message }

// AttachRefusalKind classifies an AttachRefusedError.
type AttachRefusalKind string

const (
	// AttachNotFound: the client or the server is not in the organization.
	AttachNotFound AttachRefusalKind = "not_found"
	// AttachInvalid: the server cannot offer this client.
	AttachInvalid AttachRefusalKind = "invalid"
	// AttachConflict: the server's current state refuses the client.
	AttachConflict AttachRefusalKind = "conflict"
)

// AttachToServer offers an existing auth client on another MCP server and
// records the audit event in the same transaction. It performs no
// authorization: callers check that the actor administers the organization.
// A refusal is an *AttachRefusedError.
func AttachToServer(ctx context.Context, db *pgxpool.Pool, auditLogger *audit.Logger, organizationID string, actor urn.Principal, actorDisplayName *string, clientID, serverID uuid.UUID) (*types.AuthClient, error) {
	dbtx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	client, err := queries.LockAuthClient(ctx, repo.LockAuthClientParams{ID: clientID, OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &AttachRefusedError{Code: AttachNotFound, Message: "auth client not found"}
	}
	if err != nil {
		return nil, fmt.Errorf("lock auth client: %w", err)
	}

	server, err := attachTx(ctx, queries, organizationID, client, serverID)
	if err != nil {
		return nil, err
	}
	if err := logAttach(ctx, dbtx, auditLogger, organizationID, actor, actorDisplayName, client, server); err != nil {
		return nil, err
	}

	views, err := clientViews(ctx, queries, organizationID, []repo.McpAuthClient{client})
	if err != nil {
		return nil, fmt.Errorf("build auth client view: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	return views[0], nil
}

// attachTx offers client on serverID. The caller holds the client's row lock.
// A refusal is an *AttachRefusedError.
func attachTx(ctx context.Context, queries *repo.Queries, organizationID string, client repo.McpAuthClient, serverID uuid.UUID) (repo.LockAttachableMCPServerRow, error) {
	server, err := queries.LockAttachableMCPServer(ctx, repo.LockAttachableMCPServerParams{ID: serverID, OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return server, &AttachRefusedError{Code: AttachNotFound, Message: "mcp server not found"}
	}
	if err != nil {
		return server, fmt.Errorf("lock mcp server: %w", err)
	}
	switch {
	case !server.Proxied:
		return server, &AttachRefusedError{Code: AttachInvalid, Message: "only remote and tunneled MCP servers offer auth clients; hosted toolsets keep their own authentication"}
	case server.Tunneled && (client.ClientType == authz.AuthClientTypeStaticCredential || client.ClientType == authz.AuthClientTypePassThrough):
		return server, &AttachRefusedError{Code: AttachInvalid, Message: "tunneled MCP servers are not sent a client's headers; offer them an OAuth-backed client instead"}
	case client.ProjectID.Valid && client.ProjectID.UUID != server.ProjectID:
		return server, &AttachRefusedError{Code: AttachInvalid, Message: "this client belongs to another project; only clients shared across the organization can be offered on servers in other projects"}
	case client.Enabled && server.RequiresUpstreamAuth.Valid && !server.RequiresUpstreamAuth.Bool:
		return server, &AttachRefusedError{Code: AttachConflict, Message: "the server declares that its upstream needs no authentication; change that declaration before offering it an auth client"}
	}

	if _, err := queries.AttachAuthClientToServer(ctx, repo.AttachAuthClientToServerParams{
		McpServerID:    server.ID,
		AuthClientID:   client.ID,
		OrganizationID: organizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return server, &AttachRefusedError{Code: AttachConflict, Message: "the mcp server already offers this auth client"}
		}
		return server, fmt.Errorf("attach auth client to mcp server: %w", err)
	}
	return server, nil
}

func logAttach(ctx context.Context, dbtx repo.DBTX, auditLogger *audit.Logger, organizationID string, actor urn.Principal, actorDisplayName *string, client repo.McpAuthClient, server repo.LockAttachableMCPServerRow) error {
	if err := auditLogger.LogAuthClientAttachMcpServer(ctx, dbtx, audit.LogAuthClientMcpServerEvent{
		OrganizationID:   organizationID,
		ProjectID:        server.ProjectID,
		Actor:            actor,
		ActorDisplayName: actorDisplayName,
		ActorSlug:        nil,
		AuthClientURN:    urn.NewAuthClient(client.ID),
		AuthClientName:   client.Name,
		McpServerURN:     urn.NewMcpServer(server.ID),
		McpServerName:    server.Name.String,
	}); err != nil {
		return fmt.Errorf("log auth client attach: %w", err)
	}
	return nil
}

// attachErrorResponse turns an attach failure into the handler's response.
func attachErrorResponse(ctx context.Context, logger *slog.Logger, err error) error {
	var refusal *AttachRefusedError
	if !errors.As(err, &refusal) {
		return oops.E(oops.CodeUnexpected, err, "attach auth client to mcp server").LogError(ctx, logger)
	}
	code := oops.CodeBadRequest
	switch refusal.Code {
	case AttachNotFound:
		code = oops.CodeNotFound
	case AttachConflict:
		code = oops.CodeConflict
	case AttachInvalid:
	}
	return oops.E(code, err, "%s", refusal.Message).LogWarn(ctx, logger)
}

// clientHeader is a validated header to store. value is plaintext unless
// keepStored is set, in which case it is the stored value as is.
type clientHeader struct {
	name                   string
	description            pgtype.Text
	isRequired             bool
	isSecret               bool
	value                  pgtype.Text
	valueFromRequestHeader pgtype.Text
	keepStored             bool
}

// storedHeaderValue is a header's stored value: ciphertext when isSecret,
// plaintext otherwise.
type storedHeaderValue struct {
	value    pgtype.Text
	isSecret bool
}

// headerKey is a header name's identity: names match regardless of case, and
// '_' matches '-'.
func headerKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.Trim(name, " "), "_", "-"))
}

// validateClientHeaders checks a client's complete header set. storedValues
// maps header keys of headers already storing a value to that value; a header
// given no value keeps it when it is still secret or still not secret, so a
// kept value is never reinterpreted as plaintext or ciphertext. A returned error's
// message is shown to the administrator as is, so it names the header and how
// to fix it, never a value.
func validateClientHeaders(clientType string, inputs []*gen.AuthClientHeaderInput, storedValues map[string]storedHeaderValue) ([]clientHeader, error) {
	if len(inputs) > maxHeadersPerClient {
		return nil, fmt.Errorf("an auth client can send at most %d headers", maxHeadersPerClient)
	}

	tokenGenerating := clientType == authz.AuthClientTypeRemoteSession || clientType == authz.AuthClientTypeServiceAccount
	headers := make([]clientHeader, 0, len(inputs))
	seen := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		if in == nil {
			return nil, errors.New("a header entry is empty: give each header a name and a value")
		}
		canonical, err := proxy.NormalizeHeaderName(in.Name)
		if err != nil {
			return nil, fmt.Errorf("header name %q is not a valid HTTP header name: it cannot contain spaces, control characters or separators such as ':'", in.Name)
		}
		name := strings.Trim(in.Name, " ")
		key := headerKey(canonical)
		if seen[key] {
			return nil, fmt.Errorf("the client already has a header named %q: header names match regardless of case, and '_' matches '-'", name)
		}
		seen[key] = true

		if tokenGenerating && key == "authorization" {
			return nil, fmt.Errorf("a %s client produces its own Authorization header; it cannot be configured", clientType)
		}

		value := conv.PtrValOr(in.Value, "")
		source := strings.Trim(conv.PtrValOr(in.ValueFromRequestHeader, ""), " ")
		stored, hasStored := storedValues[key]
		keep := value == "" && source == "" && hasStored && stored.isSecret == in.IsSecret

		switch {
		case keep:
		case (value == "") == (source == ""):
			return nil, fmt.Errorf("header %q must specify exactly one of value or value_from_request_header", name)
		case source != "" && in.IsSecret:
			return nil, fmt.Errorf("header %q: pass-through headers cannot be marked as secret", name)
		case clientType == authz.AuthClientTypeStaticCredential && source != "":
			return nil, fmt.Errorf("header %q: a static credential sends stored values; use a pass-through client to forward a caller's header", name)
		case clientType == authz.AuthClientTypePassThrough && value != "":
			return nil, fmt.Errorf("header %q: a pass-through client forwards the caller's headers; use a static credential to send a stored value", name)
		}

		if source != "" {
			if _, err := proxy.NormalizeHeaderName(source); err != nil {
				return nil, fmt.Errorf("header %q reads request header %q, which is not a valid HTTP header name", name, source)
			}
		}
		check := proxy.ConfiguredHeader{IsRequired: in.IsRequired, Name: name, StaticValue: value, ValueFromRequestHeader: source}
		switch err := proxy.CheckRemoteHeader(check); {
		case err == nil:
		case errors.Is(err, proxy.ErrAuthorizationSource):
			return nil, fmt.Errorf("header %q cannot read request header %q: it may carry the caller's Speakeasy credential, so it is never forwarded. Have callers send the upstream credential in a separate request header and read it from there", name, source)
		case errors.Is(err, proxy.ErrProtectedSource):
			return nil, fmt.Errorf("header %q cannot read request header %q: Speakeasy credentials and cookies are never forwarded", name, source)
		case errors.Is(err, proxy.ErrProtectedDestination):
			return nil, fmt.Errorf("header %q cannot be populated from a request header: it is a Speakeasy header", name)
		case errors.Is(err, proxy.ErrReservedHeader):
			return nil, fmt.Errorf("header %q cannot be configured: Set-Cookie, Proxy-Authorization, MCP protocol headers and the Speakeasy caller assertion are reserved, and Cookie can only hold a static value", name)
		case errors.Is(err, proxy.ErrInvalidHeaderValue):
			return nil, fmt.Errorf("the value of header %q contains a character an HTTP header cannot carry, such as a line break", name)
		default:
			return nil, fmt.Errorf("header %q: %w", name, err)
		}

		header := clientHeader{
			name:                   name,
			description:            conv.PtrToPGTextEmpty(in.Description),
			isRequired:             in.IsRequired,
			isSecret:               in.IsSecret,
			value:                  conv.ToPGTextEmpty(value),
			valueFromRequestHeader: conv.ToPGTextEmpty(source),
			keepStored:             keep,
		}
		if keep {
			header.value = stored.value
		}
		headers = append(headers, header)
	}

	switch clientType {
	case authz.AuthClientTypeStaticCredential, authz.AuthClientTypePassThrough:
		if len(headers) == 0 {
			return nil, fmt.Errorf("a %s client authenticates with its headers; add at least one", clientType)
		}
	}
	return headers, nil
}

// writeHeaders stores validated headers for client, encrypting new secret
// values.
func (s *Service) writeHeaders(ctx context.Context, queries *repo.Queries, client repo.McpAuthClient, headers []clientHeader) error {
	for _, h := range headers {
		value := h.value
		if h.isSecret && !h.keepStored && value.Valid {
			encrypted, err := s.enc.Encrypt([]byte(value.String))
			if err != nil {
				return fmt.Errorf("encrypt header %s: %w", h.name, err)
			}
			value = pgtype.Text{String: encrypted, Valid: true}
		}
		if err := queries.CreateAuthClientHeader(ctx, repo.CreateAuthClientHeaderParams{
			AuthClientID:           client.ID,
			OrganizationID:         client.OrganizationID,
			Name:                   h.name,
			Description:            h.description,
			IsRequired:             h.isRequired,
			IsSecret:               h.isSecret,
			Value:                  value,
			ValueFromRequestHeader: h.valueFromRequestHeader,
		}); err != nil {
			return fmt.Errorf("create header %s: %w", h.name, err)
		}
	}
	return nil
}

// registrationOwnerMismatch explains a registration whose credential owner
// does not fit the client type.
func registrationOwnerMismatch(clientType string) string {
	if clientType == authz.AuthClientTypeServiceAccount {
		return "a service account needs an OAuth registration that obtains its own token (client credentials); this registration has each person connect their own account"
	}
	return "a personal connection needs an OAuth registration through which each person connects their own account; this registration obtains its own token"
}
