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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// discoveryHTTPTimeout caps every outbound RFC 8414 discovery probe so a slow
// upstream cannot tie up the request handler.
const discoveryHTTPTimeout = 10 * time.Second

// rfc8414Document is the subset of the RFC 8414 / OpenID Connect Discovery
// metadata document Gram cares about for hydrating a draft.
type rfc8414Document struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	JwksURI                           string   `json:"jwks_uri"`
	ScopesSupported                   []string `json:"scopes_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
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
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").Log(ctx, logger)
	}

	parsed, err := url.Parse(issuerURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer url").Log(ctx, logger)
	}

	doc, warnings, err := discoverIssuerMetadata(ctx, s.policy, issuerURL)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "discover issuer metadata").Log(ctx, logger)
	}

	draft := &types.RemoteSessionIssuerDraft{
		Issuer:                            firstNonEmpty(doc.Issuer, issuerURL),
		AuthorizationEndpoint:             nilIfEmpty(doc.AuthorizationEndpoint),
		TokenEndpoint:                     nilIfEmpty(doc.TokenEndpoint),
		RegistrationEndpoint:              nilIfEmpty(doc.RegistrationEndpoint),
		JwksURI:                           nilIfEmpty(doc.JwksURI),
		ScopesSupported:                   doc.ScopesSupported,
		GrantTypesSupported:               doc.GrantTypesSupported,
		ResponseTypesSupported:            doc.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: doc.TokenEndpointAuthMethodsSupported,
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
		return nil, oops.E(oops.CodeBadRequest, nil, "slug is required").Log(ctx, logger)
	}
	if strings.TrimSpace(payload.Issuer) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	issuer, err := txRepo.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         *authCtx.ProjectID,
		Slug:                              payload.Slug,
		Issuer:                            payload.Issuer,
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ScopesSupported:                   nullableStringSlice(payload.ScopesSupported),
		GrantTypesSupported:               nullableStringSlice(payload.GrantTypesSupported),
		ResponseTypesSupported:            nullableStringSlice(payload.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: nullableStringSlice(payload.TokenEndpointAuthMethodsSupported),
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create remote session issuer").Log(ctx, logger)
	}

	if err := audit.LogRemoteSessionIssuerCreate(ctx, dbtx, audit.LogRemoteSessionIssuerCreateEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               *authCtx.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		RemoteSessionIssuerID:   issuer.ID,
		RemoteSessionIssuerSlug: issuer.Slug,
		IssuerURL:               issuer.Issuer,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session issuer creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: issuerID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").Log(ctx, logger)
	}

	beforeView := mv.BuildRemoteSessionIssuerView(existing)

	updated, err := txRepo.UpdateRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionIssuerParams{
		Slug:                              conv.PtrToPGText(payload.Slug),
		Issuer:                            conv.PtrToPGText(payload.Issuer),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ScopesSupported:                   nullableStringSlice(payload.ScopesSupported),
		GrantTypesSupported:               nullableStringSlice(payload.GrantTypesSupported),
		ResponseTypesSupported:            nullableStringSlice(payload.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: nullableStringSlice(payload.TokenEndpointAuthMethodsSupported),
		Oidc:                              conv.PtrToPGBool(payload.Oidc),
		Passthrough:                       conv.PtrToPGBool(payload.Passthrough),
		ID:                                issuerID,
		ProjectID:                         *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update remote session issuer").Log(ctx, logger)
	}

	afterView := mv.BuildRemoteSessionIssuerView(updated)

	if err := audit.LogRemoteSessionIssuerUpdate(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               *authCtx.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		RemoteSessionIssuerID:   updated.ID,
		RemoteSessionIssuerSlug: updated.Slug,
		IssuerURL:               updated.Issuer,
		SnapshotBefore:          beforeView,
		SnapshotAfter:           afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote session issuer update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
}

// ListRemoteSessionIssuers returns the active remote_session_issuers in the
// caller's project. Pagination params are accepted by the design but ignored
// in milestone #1; the result currently returns the full set.
func (s *Service) ListRemoteSessionIssuers(ctx context.Context, _ *gen.ListRemoteSessionIssuersPayload) (*gen.ListRemoteSessionIssuersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListRemoteSessionIssuersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session issuers").Log(ctx, s.logger)
	}

	items := make([]*types.RemoteSessionIssuer, 0, len(rows))
	for _, row := range rows {
		items = append(items, mv.BuildRemoteSessionIssuerView(row))
	}

	return &gen.ListRemoteSessionIssuersResult{
		Items:      items,
		NextCursor: nil,
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
		return nil, oops.E(oops.CodeBadRequest, nil, "exactly one of id or slug is required").Log(ctx, logger)
	}

	var issuer repo.RemoteSessionIssuer
	switch {
	case hasID:
		issuerID, err := uuid.Parse(*payload.ID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, logger)
		}
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: issuerID.String(), Dimensions: nil}); err != nil {
			return nil, err
		}
		issuer, err = repo.New(s.db).GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
			ID:        issuerID,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").Log(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").Log(ctx, logger)
		}
	default: // hasSlug
		// Slug-based lookup is project-scoped, so the project-level scope is
		// the right gate; per-resource gating happens after the row resolves.
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
			return nil, err
		}
		var err error
		issuer, err = repo.New(s.db).GetRemoteSessionIssuerBySlug(ctx, repo.GetRemoteSessionIssuerBySlugParams{
			Slug:      *payload.Slug,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").Log(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").Log(ctx, logger)
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
		return oops.E(oops.CodeBadRequest, err, "invalid issuer id").Log(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: issuerID.String(), Dimensions: nil}); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	clientCount, err := txRepo.CountRemoteSessionClientsByIssuerID(ctx, issuerID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "count remote session clients").Log(ctx, logger)
	}
	if clientCount > 0 {
		return oops.E(oops.CodeConflict, nil, "remote session issuer has active clients; delete the clients first").Log(ctx, logger)
	}

	deleted, err := txRepo.DeleteRemoteSessionIssuer(ctx, repo.DeleteRemoteSessionIssuerParams{
		ID:        issuerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "remote session issuer not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete remote session issuer").Log(ctx, logger)
	}

	if err := audit.LogRemoteSessionIssuerDelete(ctx, dbtx, audit.LogRemoteSessionIssuerDeleteEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               *authCtx.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		RemoteSessionIssuerID:   deleted.ID,
		RemoteSessionIssuerSlug: deleted.Slug,
		IssuerURL:               deleted.Issuer,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote session issuer deletion").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}

// discoverIssuerMetadata fetches and parses an RFC 8414
// .well-known/oauth-authorization-server document, returning the parsed body
// and any deviations from the spec callers should be aware of. The supplied
// guardian.Policy gates the outbound dial.
func discoverIssuerMetadata(ctx context.Context, policy *guardian.Policy, issuerURL string) (rfc8414Document, []string, error) {
	wellKnown, err := wellKnownURL(issuerURL)
	if err != nil {
		return rfc8414Document{}, nil, fmt.Errorf("compute well-known url: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, discoveryHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return rfc8414Document{}, nil, fmt.Errorf("build discovery request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := policy.Client()
	client.Timeout = discoveryHTTPTimeout
	resp, err := client.Do(req)
	if err != nil {
		return rfc8414Document{}, nil, fmt.Errorf("fetch discovery document: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return rfc8414Document{}, nil, fmt.Errorf("discovery returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return rfc8414Document{}, nil, fmt.Errorf("read discovery body: %w", err)
	}

	var doc rfc8414Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return rfc8414Document{}, nil, fmt.Errorf("decode discovery document: %w", err)
	}

	warnings := collectDiscoveryWarnings(issuerURL, doc)

	return doc, warnings, nil
}

// wellKnownURL composes the RFC 8414 .well-known path for an issuer. RFC 8414
// places the path immediately after the host (scheme://host/.well-known/...);
// any path component on the issuer is appended to the well-known URL.
func wellKnownURL(issuerURL string) (string, error) {
	u, err := url.Parse(issuerURL)
	if err != nil {
		return "", fmt.Errorf("parse issuer url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("issuer url must include scheme and host")
	}

	path := strings.TrimSuffix(u.Path, "/")
	wellKnown := *u
	wellKnown.Path = "/.well-known/oauth-authorization-server" + path
	wellKnown.RawQuery = ""
	wellKnown.Fragment = ""

	return wellKnown.String(), nil
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

// firstNonEmpty returns the first argument that is non-empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// nilIfEmpty returns nil for an empty string, otherwise a pointer to it.
func nilIfEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// nullableStringSlice is the sqlc COALESCE pattern: pass nil to keep the
// existing column value, pass a (possibly empty) slice to overwrite it.
func nullableStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	return s
}
