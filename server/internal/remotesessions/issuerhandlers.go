package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// discoveryHTTPTimeout caps the whole issuer discovery sequence — every
// candidate probe shares this single budget — so a slow upstream cannot tie up
// the request handler.
const discoveryHTTPTimeout = 10 * time.Second

// rfc8414Document is the subset of the RFC 8414 / OpenID Connect Discovery
// metadata document Gram cares about for hydrating a draft.
type rfc8414Document struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	JwksURI                           string   `json:"jwks_uri"`
	ServiceDocumentation              string   `json:"service_documentation"`
	OpPolicyURI                       string   `json:"op_policy_uri"`
	OpTosURI                          string   `json:"op_tos_uri"`
	ScopesSupported                   []string `json:"scopes_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	// ClientIDMetadataDocumentSupported comes from the OAuth CIMD draft
	// (draft-ietf-oauth-client-id-metadata-document), not base RFC 8414: whether
	// the issuer accepts a Client ID Metadata Document URL as client_id. Used to
	// pre-flight outbound CIMD opt-in.
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported"`
}

// DiscoverRemoteSessionIssuer fetches the upstream issuer's RFC 8414 metadata
// document and returns a draft suitable for createRemoteSessionIssuer. No
// persistence; the caller decides whether the draft is worth storing.
func (s *Service) DiscoverRemoteSessionIssuer(ctx context.Context, payload *gen.DiscoverRemoteSessionIssuerPayload) (*types.RemoteSessionIssuerDraft, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerURL := strings.TrimSpace(payload.Issuer)
	if issuerURL == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").LogError(ctx, logger)
	}

	if !urls.IsAbsoluteHTTP(issuerURL) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid issuer url").LogError(ctx, logger)
	}

	doc, warnings, err := discoverIssuerMetadata(ctx, s.policy, issuerURL)
	if err != nil {
		if df, ok := errors.AsType[*discoveryError](err); ok {
			return nil, oops.E(oops.CodeGatewayError, err, "%s", df.UserMessage()).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeGatewayError, err, "discover issuer metadata").LogError(ctx, logger)
	}

	draft := &types.RemoteSessionIssuerDraft{
		Issuer:                conv.Default(doc.Issuer, issuerURL),
		AuthorizationEndpoint: conv.PtrEmpty(doc.AuthorizationEndpoint),
		TokenEndpoint:         conv.PtrEmpty(doc.TokenEndpoint),
		RegistrationEndpoint:  conv.PtrEmpty(doc.RegistrationEndpoint),
		JwksURI:               conv.PtrEmpty(doc.JwksURI),
		// The issuer controls these and downstream surfaces render them as links,
		// so a value that is not an absolute http(s) URL is discarded rather than
		// carried into the draft the create form submits back.
		ServiceDocumentation:              conv.PtrEmpty(conv.Ternary(urls.IsAbsoluteHTTP(doc.ServiceDocumentation), doc.ServiceDocumentation, "")),
		OpPolicyURI:                       conv.PtrEmpty(conv.Ternary(urls.IsAbsoluteHTTP(doc.OpPolicyURI), doc.OpPolicyURI, "")),
		OpTosURI:                          conv.PtrEmpty(conv.Ternary(urls.IsAbsoluteHTTP(doc.OpTosURI), doc.OpTosURI, "")),
		ScopesSupported:                   doc.ScopesSupported,
		GrantTypesSupported:               doc.GrantTypesSupported,
		ResponseTypesSupported:            doc.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: doc.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: doc.ClientIDMetadataDocumentSupported,
		Oidc:                              false,
		Passthrough:                       false,
		DiscoveryWarnings:                 warnings,
	}

	return draft, nil
}

// CreateRemoteSessionIssuer persists a new remote_session_issuer in the
// caller's project. The slug must be unique per project.
func (s *Service) CreateRemoteSessionIssuer(ctx context.Context, payload *gen.CreateRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if strings.TrimSpace(payload.Slug) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").LogError(ctx, logger)
	}
	if strings.TrimSpace(payload.Issuer) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").LogError(ctx, logger)
	}

	// Operator-supplied and later rendered as a link, so it is validated here.
	// An empty value stays legal: the create query stores it as NULL.
	if v := conv.PtrValOr(payload.ClientSetupDocumentationURL, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_setup_documentation_url must be an absolute http(s) URL").LogError(ctx, logger)
	}

	logoAssetID, err := conv.PtrToNullUUID(payload.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
	}

	// Discovery drops malformed documentation URLs, but a caller holding the write
	// scope can POST them without ever calling discover, and they are persisted
	// and later rendered as links. An empty value stays legal: the update queries
	// read it as the explicit "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ServiceDocumentation, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "service_documentation must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpPolicyURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_policy_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpTosURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_tos_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	issuer, err := txRepo.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              payload.Slug,
		Issuer:                            payload.Issuer,
		Name:                              conv.PtrToPGTextTrimmed(payload.Name),
		LogoAssetID:                       logoAssetID,
		ClientSetupDocumentationUrl:       conv.PtrToPGTextEmpty(payload.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGTextEmpty(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGTextEmpty(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGTextEmpty(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrValOr(payload.ClientIDMetadataDocumentSupported, false),
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
	})
	if err != nil {
		if isRemoteSessionIssuerSlugConflict(err) {
			return nil, oops.E(oops.CodeConflict, err, "an issuer with this slug already exists").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create remote session issuer").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionIssuerCreate(ctx, dbtx, audit.LogRemoteSessionIssuerCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(issuer.ID),
		Slug:                   issuer.Slug,
		IssuerURL:              issuer.Issuer,
		Name:                   conv.FromPGText[string](issuer.Name),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session issuer creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// UpdateRemoteSessionIssuer applies an optional patch to an existing
// remote_session_issuer.
func (s *Service) UpdateRemoteSessionIssuer(ctx context.Context, payload *gen.UpdateRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	// slug and issuer are NOT NULL on the row. The SQL update treats an
	// explicit empty string as "clear to NULL" for the nullable endpoint and
	// documentation columns, but applying that to slug/issuer would violate the
	// constraint, so reject empty here with an actionable error before the
	// query runs.
	if payload.Slug != nil && *payload.Slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "slug cannot be set to empty").LogError(ctx, logger)
	}
	if payload.Issuer != nil && *payload.Issuer == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer cannot be set to empty").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	// Operator-supplied and later rendered as a link, so it is validated here.
	// An empty value stays legal: the update query reads it as the explicit
	// "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ClientSetupDocumentationURL, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_setup_documentation_url must be an absolute http(s) URL").LogError(ctx, logger)
	}

	logoAssetID, err := conv.PtrToNullUUID(payload.LogoAssetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
	}

	// Discovery drops malformed documentation URLs, but a caller holding the write
	// scope can POST them without ever calling discover, and they are persisted
	// and later rendered as links. An empty value stays legal: the update queries
	// read it as the explicit "clear to NULL" sentinel.
	if v := conv.PtrValOr(payload.ServiceDocumentation, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "service_documentation must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpPolicyURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_policy_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}
	if v := conv.PtrValOr(payload.OpTosURI, ""); v != "" && !urls.IsAbsoluteHTTP(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "op_tos_uri must be an absolute http(s) URL").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Keep the pre-update lookup strictly project-scoped: organization-level
	// issuers are edited via the organizationRemoteSessionIssuers service, and
	// the project-scoped UpdateRemoteSessionIssuer below cannot modify them.
	existing, err := txRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:             issuerID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID: conv.ToPGTextEmpty(""),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
	}

	beforeView := mv.BuildRemoteSessionIssuerView(existing)

	updated, err := txRepo.UpdateRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionIssuerParams{
		Slug:                              conv.PtrToPGText(payload.Slug),
		Issuer:                            conv.PtrToPGText(payload.Issuer),
		Name:                              conv.PtrToPGText(payload.Name),
		LogoAssetID:                       logoAssetID,
		ClientSetupDocumentationUrl:       conv.PtrToPGText(payload.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGText(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGText(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGText(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrToPGBool(payload.ClientIDMetadataDocumentSupported),
		Oidc:                              conv.PtrToPGBool(payload.Oidc),
		Passthrough:                       conv.PtrToPGBool(payload.Passthrough),
		ID:                                issuerID,
		ProjectID:                         uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update remote session issuer").LogError(ctx, logger)
	}

	afterView := mv.BuildRemoteSessionIssuerView(updated)

	if err := s.auditLogger.LogRemoteSessionIssuerUpdate(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(updated.ID),
		Slug:                   updated.Slug,
		IssuerURL:              updated.Issuer,
		Name:                   conv.FromPGText[string](updated.Name),
		SnapshotBefore:         beforeView,
		SnapshotAfter:          afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session issuer update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) ListRemoteSessionIssuers(ctx context.Context, payload *gen.ListRemoteSessionIssuersPayload) (*gen.ListRemoteSessionIssuersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := pageLimit(payload.Limit)
	cursor, err := parseCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
	}

	rows, err := repo.New(s.db).ListRemoteSessionIssuersByProjectID(ctx, repo.ListRemoteSessionIssuersByProjectIDParams{
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Cursor:         cursor,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session issuers").LogError(ctx, s.logger)
	}

	items := make([]*types.RemoteSessionIssuer, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildRemoteSessionIssuerView(row))
	}

	var nextCursor *string
	if len(rows) >= int(limit) {
		c := rows[len(rows)-1].ID.String()
		nextCursor = &c
	}

	return &gen.ListRemoteSessionIssuersResult{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetRemoteSessionIssuer resolves a single issuer by either id or slug.
// Exactly one of the two must be supplied.
func (s *Service) GetRemoteSessionIssuer(ctx context.Context, payload *gen.GetRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	hasID := payload.ID != nil && *payload.ID != ""
	hasSlug := payload.Slug != nil && *payload.Slug != ""
	if hasID == hasSlug {
		return nil, oops.E(oops.CodeBadRequest, nil, "exactly one of id or slug is required").LogError(ctx, logger)
	}

	var issuer repo.RemoteSessionIssuer
	switch {
	case hasID:
		issuerID, err := uuid.Parse(*payload.ID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
		}
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
			return nil, err
		}
		issuer, err = repo.New(s.db).GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
			ID:             issuerID,
			ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
		}
	default: // hasSlug
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
			return nil, err
		}
		var err error
		issuer, err = repo.New(s.db).GetRemoteSessionIssuerBySlug(ctx, repo.GetRemoteSessionIssuerBySlugParams{
			Slug:      *payload.Slug,
			ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
		}
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// DeleteRemoteSessionIssuer soft-deletes an issuer. Blocked when any
// non-deleted remote_session_clients still reference it.
func (s *Service) DeleteRemoteSessionIssuer(ctx context.Context, payload *gen.DeleteRemoteSessionIssuerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	clientCount, err := txRepo.CountRemoteSessionClientsByIssuerID(ctx, issuerID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "count remote session clients").LogError(ctx, logger)
	}
	if clientCount > 0 {
		return oops.E(oops.CodeConflict, nil, "remote session issuer has active clients; delete the clients first").LogError(ctx, logger)
	}

	deleted, err := txRepo.DeleteRemoteSessionIssuer(ctx, repo.DeleteRemoteSessionIssuerParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete remote session issuer").LogError(ctx, logger)
	}

	if err := s.auditLogger.LogRemoteSessionIssuerDelete(ctx, dbtx, audit.LogRemoteSessionIssuerDeleteEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(deleted.ID),
		Slug:                   deleted.Slug,
		IssuerURL:              deleted.Issuer,
		Name:                   conv.FromPGText[string](deleted.Name),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote session issuer deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// discoveryError captures enough context about a failed RFC 8414 fetch that
// the handler can compose a user-facing message naming the well-known URL and,
// when available, the upstream HTTP status. Status is zero when no HTTP
// response was received (transport error, malformed URL, etc.).
type discoveryError struct {
	WellKnownURL string
	Status       int
	cause        error
}

func (e *discoveryError) Error() string {
	switch {
	case e.WellKnownURL == "":
		return e.cause.Error()
	case e.Status > 0:
		return fmt.Sprintf("discover %s: HTTP %d: %s", e.WellKnownURL, e.Status, e.cause)
	default:
		return fmt.Sprintf("discover %s: %s", e.WellKnownURL, e.cause)
	}
}

func (e *discoveryError) Unwrap() error { return e.cause }

// UserMessage produces the public, user-facing summary surfaced through the
// management API. Callers wrap it in an oops.E to attach the gateway error
// code and id.
func (e *discoveryError) UserMessage() string {
	switch {
	case e.Status == http.StatusNotFound:
		return fmt.Sprintf("OAuth metadata not found at %s", e.WellKnownURL)
	case e.Status >= 400:
		return fmt.Sprintf("Unexpected HTTP %d from %s", e.Status, e.WellKnownURL)
	case e.Status == http.StatusOK:
		// 200 made it back but the body was unreadable or malformed.
		return fmt.Sprintf("OAuth metadata at %s was not a valid RFC 8414 document", e.WellKnownURL)
	case e.WellKnownURL != "":
		return fmt.Sprintf("Could not reach OAuth metadata at %s", e.WellKnownURL)
	default:
		return "Could not compute OAuth metadata URL for the supplied issuer"
	}
}

// discoverIssuerMetadata fetches and parses an issuer's RFC 8414 / OpenID
// Connect Discovery metadata document, returning the parsed body and any
// deviations from the spec callers should be aware of. The supplied
// guardian.Policy gates the outbound dial.
//
// It probes the well-known locations returned by issuerProbeCandidates in
// order, returning the first that yields a usable document — one carrying both
// an authorization_endpoint and a token_endpoint. A 200 that parses but lacks
// those endpoints is almost always a SPA/gateway catch-all answering our
// speculative candidate rather than real metadata, so it is skipped in favor of
// a later candidate (e.g. the origin-style fallback); it is surfaced only as a
// last resort when no candidate yields a usable document. When every probe
// fails the first (canonical RFC 8414) candidate's error is surfaced, wrapped
// in a *discoveryError so the handler can attach the upstream URL and status to
// the user-facing error.
func discoverIssuerMetadata(ctx context.Context, policy *guardian.Policy, issuerURL string) (rfc8414Document, []string, error) {
	candidates, err := issuerProbeCandidates(issuerURL)
	if err != nil {
		return rfc8414Document{}, nil, &discoveryError{
			WellKnownURL: "",
			Status:       0,
			cause:        fmt.Errorf("compute well-known url: %w", err),
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, discoveryHTTPTimeout)
	defer cancel()

	client := policy.Client()

	var firstErr *discoveryError
	var fallbackDoc rfc8414Document
	haveFallback := false
	for _, wellKnown := range candidates {
		doc, attemptErr := attemptIssuerProbe(reqCtx, client, wellKnown)
		if attemptErr != nil {
			if firstErr == nil {
				firstErr = attemptErr
			}
			continue
		}

		// A 200 that parses but advertises no usable OAuth endpoints is almost
		// always a catch-all answering our speculative candidate, not real
		// metadata. Remember the first such document but keep probing — a later
		// candidate (e.g. the origin-style fallback) may carry the real one.
		if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
			if !haveFallback {
				fallbackDoc = doc
				haveFallback = true
			}
			continue
		}

		return doc, collectDiscoveryWarnings(issuerURL, doc), nil
	}

	if haveFallback {
		return fallbackDoc, collectDiscoveryWarnings(issuerURL, fallbackDoc), nil
	}

	return rfc8414Document{}, nil, firstErr
}

// attemptIssuerProbe issues a single GET against an issuer well-known URL and
// returns either the parsed RFC 8414 / OIDC document or a typed error annotated
// with the probed URL and upstream status.
func attemptIssuerProbe(ctx context.Context, client *guardian.HTTPClient, wellKnown string) (rfc8414Document, *discoveryError) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return rfc8414Document{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       0,
			cause:        fmt.Errorf("build discovery request: %w", err),
		}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return rfc8414Document{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       0,
			cause:        fmt.Errorf("fetch discovery document: %w", err),
		}
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return rfc8414Document{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       resp.StatusCode,
			cause:        fmt.Errorf("discovery returned status %d", resp.StatusCode),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return rfc8414Document{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       resp.StatusCode,
			cause:        fmt.Errorf("read discovery body: %w", err),
		}
	}

	var doc rfc8414Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return rfc8414Document{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       resp.StatusCode,
			cause:        fmt.Errorf("decode discovery document: %w", err),
		}
	}

	return doc, nil
}

// issuerProbeCandidates returns the ordered list of well-known metadata URLs to
// probe for an issuer. The first candidate is the canonical RFC 8414 location;
// the rest broaden coverage to OpenID Connect Discovery and to non-compliant
// upstreams that only serve metadata at the origin root.
//
// RFC 8414 §3 inserts the well-known path between the host and the issuer path;
// OpenID Connect Discovery appends "/.well-known/openid-configuration" after the
// issuer. Many identity providers (Auth0, Okta, Google, Azure AD, Keycloak)
// serve only the OIDC document, so it is always probed. When the issuer has a
// path component we additionally fall back to the origin-style locations, since
// some gateways and SPA catch-alls serve metadata at the root regardless of the
// issuer path. Duplicate URLs (e.g. when the issuer has no path) are collapsed.
func issuerProbeCandidates(issuerURL string) ([]string, error) {
	u, err := url.Parse(issuerURL)
	if err != nil {
		return nil, fmt.Errorf("parse issuer url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("issuer url must include scheme and host")
	}

	origin := (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
	path := strings.TrimSuffix(u.Path, "/")

	seen := make(map[string]struct{})
	candidates := make([]string, 0, 5)
	add := func(raw string) {
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		candidates = append(candidates, raw)
	}

	// RFC 8414 §3: well-known inserted between host and issuer path.
	add(origin + "/.well-known/oauth-authorization-server" + path)
	// RFC 8414 §3.1 OIDC-compatible form: openid-configuration inserted between
	// host and issuer path.
	add(origin + "/.well-known/openid-configuration" + path)
	if path != "" {
		// OpenID Connect Discovery: well-known appended after the issuer path.
		add(origin + path + "/.well-known/openid-configuration")
		// Origin-style fallback: strip the issuer path entirely.
		add(origin + "/.well-known/oauth-authorization-server")
		add(origin + "/.well-known/openid-configuration")
	}

	return candidates, nil
}

// collectDiscoveryWarnings reports RFC 8414 deviations on the parsed metadata
// document. The list is informational; discover never fails on these.
func collectDiscoveryWarnings(requestedIssuer string, doc rfc8414Document) []string {
	warnings := []string{}
	if doc.Issuer == "" {
		warnings = append(warnings, "issuer field missing from discovery document")
	} else if !issuerURLsEqual(doc.Issuer, requestedIssuer) {
		warnings = append(warnings, fmt.Sprintf("discovery issuer %q does not match requested %q", doc.Issuer, requestedIssuer))
	}
	if doc.AuthorizationEndpoint == "" {
		warnings = append(warnings, "authorization_endpoint missing from discovery document")
	}
	if doc.TokenEndpoint == "" {
		warnings = append(warnings, "token_endpoint missing from discovery document")
	}
	if doc.JwksURI == "" {
		warnings = append(warnings, "jwks_uri missing from discovery document")
	}
	return warnings
}

// issuerURLsEqual compares two issuer URLs ignoring trailing slashes.
func issuerURLsEqual(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// pageLimit clamps the user-supplied limit into the documented range and
// returns it as an int32 ready for sqlc parameters. The clamp guarantees the
// value stays within int32 range.
func pageLimit(in *int) int32 {
	limit := constants.DefaultPageLimit
	if in != nil {
		limit = *in
	}
	if limit <= 0 {
		limit = constants.DefaultPageLimit
	}
	if limit > constants.MaxPageLimit {
		limit = constants.MaxPageLimit
	}
	return int32(limit)
}

// parseCursor decodes a list cursor. Cursors are the id of the last row
// on the previous page; an empty/nil cursor means "start of list".
func parseCursor(cursor *string) (uuid.NullUUID, error) {
	if cursor == nil || *cursor == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(*cursor)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("parse cursor: %w", err)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}
