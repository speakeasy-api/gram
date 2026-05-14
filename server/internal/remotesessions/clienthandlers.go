package remotesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// dcrHTTPTimeout caps every outbound RFC 7591 registration call so a slow
// upstream cannot tie up the request handler.
const dcrHTTPTimeout = 10 * time.Second

// dcrMaxBodyBytes bounds the registration response body to keep a hostile
// upstream from exhausting memory. Currently 1 MiB.
const dcrMaxBodyBytes = 1 << 20

// rfc7591Request is the subset of an RFC 7591 Dynamic Client Registration
// request body Gram sends when auto-registering against an issuer's
// registration_endpoint.
type rfc7591Request struct {
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// dcrParams collects the inputs the RFC 7591 helper accepts. ClientName,
// RedirectURIs are optional caller overrides; Scopes is the union of scopes
// the issuer advertised.
type dcrParams struct {
	RegistrationEndpoint string
	Scopes               []string
	ClientName           string
	RedirectURIs         []string
}

// rfc7591Response is the subset of an RFC 7591 registration response Gram
// persists. client_id_issued_at and client_secret_expires_at are seconds since
// the Unix epoch per the spec.
type rfc7591Response struct {
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at,omitempty"`
}

func (s *Service) CreateRemoteSessionClient(ctx context.Context, payload *gen.CreateRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").Log(ctx, logger)
	}

	userIssuerID, err := uuid.Parse(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").Log(ctx, logger)
	}

	autoRegister := conv.PtrValOr(payload.AutoRegister, false)
	hasClientID := payload.ClientID != nil && strings.TrimSpace(*payload.ClientID) != ""

	if autoRegister && hasClientID {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id and auto_register are mutually exclusive").Log(ctx, logger)
	}
	if !autoRegister && !hasClientID {
		return nil, oops.E(oops.CodeBadRequest, nil, "either client_id or auto_register must be supplied").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	issuer, err := txRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").Log(ctx, logger)
	}

	var (
		clientID         string
		secretCiphertext pgtype.Text
		issuedAt         pgtype.Timestamptz
		expiresAt        pgtype.Timestamptz
	)

	switch {
	case autoRegister:
		regEndpoint := conv.FromPGTextOrEmpty[string](issuer.RegistrationEndpoint)
		if regEndpoint == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "issuer has no registration_endpoint; auto_register unavailable").Log(ctx, logger)
		}

		dcrResp, err := registerClientViaDCR(ctx, s.policy, dcrParams{
			RegistrationEndpoint: regEndpoint,
			Scopes:               issuer.ScopesSupported,
			ClientName:           "",
			RedirectURIs:         nil,
		})
		if err != nil {
			return nil, oops.E(oops.CodeGatewayError, err, "dynamic client registration failed").Log(ctx, logger)
		}

		clientID = dcrResp.ClientID
		if dcrResp.ClientSecret != "" {
			encrypted, encErr := s.enc.Encrypt([]byte(dcrResp.ClientSecret))
			if encErr != nil {
				return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").Log(ctx, logger)
			}
			secretCiphertext = conv.ToPGText(encrypted)
		}
		if dcrResp.ClientIDIssuedAt > 0 {
			issuedAt = conv.ToPGTimestamptz(time.Unix(dcrResp.ClientIDIssuedAt, 0).UTC())
		}
		if dcrResp.ClientSecretExpiresAt > 0 {
			expiresAt = conv.ToPGTimestamptz(time.Unix(dcrResp.ClientSecretExpiresAt, 0).UTC())
		}
	default:
		clientID = strings.TrimSpace(*payload.ClientID)
		if payload.ClientSecret != nil && *payload.ClientSecret != "" {
			encrypted, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
			if encErr != nil {
				return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").Log(ctx, logger)
			}
			secretCiphertext = conv.ToPGText(encrypted)
		}
		issuedAt = conv.ToPGTimestamptz(time.Now().UTC())
	}

	created, err := txRepo.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:             *authCtx.ProjectID,
		RemoteSessionIssuerID: issuerID,
		UserSessionIssuerID:   userIssuerID,
		ClientID:              clientID,
		ClientSecretEncrypted: secretCiphertext,
		ClientIDIssuedAt:      issuedAt,
		ClientSecretExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create remote session client").Log(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientCreate(ctx, dbtx, audit.LogRemoteSessionClientCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(created.ID),
		ClientID:               created.ClientID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session client creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return mv.BuildRemoteSessionClientView(created), nil
}

func (s *Service) UpdateRemoteSessionClient(ctx context.Context, payload *gen.UpdateRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").Log(ctx, logger)
	}

	userIssuerID, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:        clientID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").Log(ctx, logger)
	}

	beforeView := mv.BuildRemoteSessionClientView(existing)

	var secretCiphertext pgtype.Text
	if payload.ClientSecret != nil && *payload.ClientSecret != "" {
		encrypted, encErr := s.enc.Encrypt([]byte(*payload.ClientSecret))
		if encErr != nil {
			return nil, oops.E(oops.CodeUnexpected, encErr, "encrypt client secret").Log(ctx, logger)
		}
		secretCiphertext = conv.ToPGText(encrypted)
	}

	updated, err := txRepo.UpdateRemoteSessionClient(ctx, repo.UpdateRemoteSessionClientParams{
		ClientSecretEncrypted: secretCiphertext,
		ClientSecretExpiresAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		UserSessionIssuerID:   userIssuerID,
		ID:                    clientID,
		ProjectID:             *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update remote session client").Log(ctx, logger)
	}

	afterView := mv.BuildRemoteSessionClientView(updated)

	if err := s.auditLogger.LogRemoteSessionClientUpdate(ctx, dbtx, audit.LogRemoteSessionClientUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(updated.ID),
		ClientID:               updated.ClientID,
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session client update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) ListRemoteSessionClients(ctx context.Context, payload *gen.ListRemoteSessionClientsPayload) (*gen.ListRemoteSessionClientsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerFilter, err := conv.PtrToNullUUID(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_issuer_id").Log(ctx, logger)
	}

	userIssuerFilter, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").Log(ctx, logger)
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, logger)
	}

	rows, err := repo.New(s.db).ListRemoteSessionClientsByProjectID(ctx, repo.ListRemoteSessionClientsByProjectIDParams{
		ProjectID:             *authCtx.ProjectID,
		RemoteSessionIssuerID: issuerFilter,
		UserSessionIssuerID:   userIssuerFilter,
		Cursor:                cursor,
		LimitValue:            limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session clients").Log(ctx, logger)
	}

	items := make([]*types.RemoteSessionClient, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildRemoteSessionClientView(row))
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &gen.ListRemoteSessionClientsResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) GetRemoteSessionClient(ctx context.Context, payload *gen.GetRemoteSessionClientPayload) (*types.RemoteSessionClient, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").Log(ctx, logger)
	}

	client, err := repo.New(s.db).GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:        clientID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").Log(ctx, logger)
	}

	return mv.BuildRemoteSessionClientView(client), nil
}

// DeleteRemoteSessionClient soft-deletes a client and cascades the soft-delete
// to the upstream-token rows pointing at it in the same transaction. The FK
// has ON DELETE CASCADE, but since we soft-delete the parent, dependent rows
// would otherwise stay reachable; force them out of any active set here.
func (s *Service) DeleteRemoteSessionClient(ctx context.Context, payload *gen.DeleteRemoteSessionClientPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client id").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteRemoteSessionClient(ctx, repo.DeleteRemoteSessionClientParams{
		ID:        clientID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete remote session client").Log(ctx, logger)
	}

	if _, err := txRepo.SoftDeleteRemoteSessionsByClientID(ctx, deleted.ID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft-delete dependent remote sessions").Log(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionClientDelete(ctx, dbtx, audit.LogRemoteSessionClientDeleteEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionClientURN: urn.NewRemoteSessionClient(deleted.ID),
		ClientID:               deleted.ClientID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote session client deletion").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}

// registerClientViaDCR fires an RFC 7591 Dynamic Client Registration request
// against the issuer's registration_endpoint. The guardian.Policy supplies an
// SSRF-gated HTTP client.
func registerClientViaDCR(ctx context.Context, policy *guardian.Policy, params dcrParams) (rfc7591Response, error) {
	clientName := params.ClientName
	if strings.TrimSpace(clientName) == "" {
		clientName = "Gram"
	}
	reqBody, err := json.Marshal(rfc7591Request{
		ClientName:              clientName,
		RedirectURIs:            params.RedirectURIs,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "client_secret_basic",
		Scope:                   strings.Join(params.Scopes, " "),
	})
	if err != nil {
		return rfc7591Response{}, fmt.Errorf("marshal dcr request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, dcrHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, params.RegistrationEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return rfc7591Response{}, fmt.Errorf("build dcr request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := policy.Client().Do(req)
	if err != nil {
		return rfc7591Response{}, fmt.Errorf("dcr request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, dcrMaxBodyBytes))
	if err != nil {
		return rfc7591Response{}, fmt.Errorf("read dcr response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return rfc7591Response{}, fmt.Errorf("dcr returned status %d: %s", resp.StatusCode, string(body))
	}

	var dcr rfc7591Response
	if err := json.Unmarshal(body, &dcr); err != nil {
		return rfc7591Response{}, fmt.Errorf("decode dcr response: %w", err)
	}

	if strings.TrimSpace(dcr.ClientID) == "" {
		return rfc7591Response{}, fmt.Errorf("dcr response missing client_id")
	}

	return dcr, nil
}
