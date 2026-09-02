package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
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
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	JwksURI                           string   `json:"jwks_uri"`
	ServiceDocumentation              string   `json:"service_documentation"`
	OpPolicyURI                       string   `json:"op_policy_uri"`
	OpTosURI                          string   `json:"op_tos_uri"`
	ScopesSupported                   []string `json:"scopes_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`

	// CodeChallengeMethodsSupported stays nil when the document omits the
	// field, and only here: persistence collapses it to an empty array
	// ("captured; the upstream advertises nothing", RFC 8414's stated meaning
	// for absence) while the column's NULL is reserved for rows discovery has
	// not captured at all. The nil/empty split survives just long enough for
	// collectDiscoveryWarnings to word the two cases differently.
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`

	// ClientIDMetadataDocumentSupported comes from the OAuth CIMD draft
	// (draft-ietf-oauth-client-id-metadata-document), not base RFC 8414: whether
	// the issuer accepts a Client ID Metadata Document URL as client_id. Used to
	// pre-flight outbound CIMD opt-in.
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported"`
}

// FetchRemoteSessionIssuerMetadata fetches the upstream issuer's RFC 8414
// metadata document and returns a draft suitable for createRemoteSessionIssuer.
// Keyed by issuer URL, so no record need exist; nothing is persisted and the
// caller decides whether the draft is worth storing. RefreshRemoteSessionIssuerMetadata
// is the persisting counterpart for an issuer that already exists.
func (s *Service) FetchRemoteSessionIssuerMetadata(ctx context.Context, payload *gen.FetchRemoteSessionIssuerMetadataPayload) (*types.RemoteSessionIssuerDraft, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// No project:read/write check: this handler never reads or writes any
	// project-owned data, it only fetches and reflects back the caller-supplied
	// issuer's own public RFC 8414 discovery document (nothing persisted, see
	// the doc comment above). There is no project resource here to gate.
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerURL := strings.TrimSpace(payload.Issuer)
	if issuerURL == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "issuer is required").LogError(ctx, logger)
	}

	if !urls.IsAbsoluteHTTP(issuerURL) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid issuer url").LogError(ctx, logger)
	}

	doc, _, warnings, err := discoverIssuerMetadata(ctx, s.policy, issuerURL)
	if err != nil {
		return nil, mapDiscoveryError(ctx, logger, err, oops.CodeBadRequest)
	}

	return buildIssuerDraft(doc, issuerURL, warnings), nil
}

// RefreshRemoteSessionIssuerMetadata re-reads a project-owned issuer's RFC 8414
// metadata document and persists the discovered values, returning the updated
// issuer alongside any warnings.
//
// Organization-level issuers are inherited into projects for reading but are
// not writable here; they refresh through
// organizationRemoteSessionIssuers.refreshMetadata, matching how
// UpdateRemoteSessionIssuer scopes its write.
func (s *Service) RefreshRemoteSessionIssuerMetadata(ctx context.Context, payload *gen.RefreshRemoteSessionIssuerMetadataPayload) (*types.RemoteSessionIssuerRefresh, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid issuer id").LogError(ctx, logger)
	}

	// project:write, not project:read: this persists.
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	// Loaded before the transaction opens, because discovery below is an
	// upstream HTTP call under a ten-second budget and must not run while
	// holding a pooled connection. The update re-asserts this row's identity,
	// so a concurrent move or issuer rename aborts the write rather than
	// letting the gap be exploited.
	existing, err := repo.New(s.db).GetRemoteSessionIssuerByIDProjectOwned(ctx, repo.GetRemoteSessionIssuerByIDProjectOwnedParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
	}

	params, warnings, err := refreshIssuerMetadata(ctx, s.policy, existing)
	if err != nil {
		return nil, mapDiscoveryError(ctx, logger, err, oops.CodeGatewayError)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Re-read under a row lock rather than reusing the pre-discovery read: an
	// updateIssuer that committed while discovery ran would otherwise land in
	// this entry's before/after diff and be attributed to the refresh. The lock
	// is taken after discovery finished, so it is never held across the upstream
	// call.
	locked, err := txRepo.GetRemoteSessionIssuerByIDForUpdate(ctx, repo.GetRemoteSessionIssuerByIDForUpdateParams{
		ID:        issuerID,
		ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, err, "%s", refreshConflictMessage).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock remote session issuer").LogError(ctx, logger)
	}

	beforeView := mv.BuildRemoteSessionIssuerView(locked)

	updated, err := txRepo.UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeConflict, err, "%s", refreshConflictMessage).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update remote session issuer discovered metadata").LogError(ctx, logger)
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

	return &types.RemoteSessionIssuerRefresh{Issuer: afterView, DiscoveryWarnings: warnings}, nil
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

	// Revocation endpoint must be HTTPS, or HTTP on loopback where a token
	// never crosses a network: tokens are sensitive credentials that must not
	// be transmitted in plaintext. An empty value stays legal.
	if v := conv.PtrValOr(payload.RevocationEndpoint, ""); v != "" && !urls.IsAbsoluteHTTPSOrLoopback(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "revocation_endpoint must be an absolute https URL, or http on loopback").LogError(ctx, logger)
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
		RevocationEndpoint:                conv.PtrToPGText(payload.RevocationEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGTextEmpty(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGTextEmpty(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGTextEmpty(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		CodeChallengeMethodsSupported:     payload.CodeChallengeMethodsSupported,
		ClientIDMetadataDocumentSupported: conv.PtrValOr(payload.ClientIDMetadataDocumentSupported, false),
		Oidc:                              conv.PtrValOr(payload.Oidc, false),
		Passthrough:                       conv.PtrValOr(payload.Passthrough, false),
		// Create does not discover, so it has no document to snapshot. The
		// column is filled by the first refresh, and by callers that already
		// hold a discovery document (see platformmcp's attachment).
		Metadata: nil,
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

// resolveIssuerByPrecedence picks the single issuer a project should use out of
// every tier-visible candidate matching one upstream URL.
//
// Precedence is project > organization > platform, ranked by scopeOf so the
// ladder has one definition shared with issuer migration. Row order carries no
// tier information, so it can never stand in for this.
//
// Within a tier the oldest issuer wins. That tie-break is load-bearing rather
// than cosmetic: several project-tier rows on one URL are normal, because the
// manual attach form creates unconditionally and always has. Without it the
// endpoint would return whichever row the planner happened to emit first and
// two identical calls could disagree. Candidates arrive ordered oldest-first by
// created_at, so keeping the first of the best tier is "oldest" — see the query
// for why id ordering is not a substitute.
func resolveIssuerByPrecedence(candidates []repo.RemoteSessionIssuer) (repo.RemoteSessionIssuer, bool) {
	var best repo.RemoteSessionIssuer
	found := false

	for _, candidate := range candidates {
		if !found || scopeOf(candidate) < scopeOf(best) {
			best = candidate
			found = true
		}
	}

	return best, found
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

	// An empty logo asset id stays legal: the update query reads it as the
	// explicit "clear to NULL" sentinel. Any other value must be a uuid —
	// the query casts the text parameter, so a malformed value has to be
	// rejected here rather than surfacing as a Postgres cast error.
	if v := conv.PtrValOr(payload.LogoAssetID, ""); v != "" {
		if _, err := uuid.Parse(v); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid logo asset id").LogError(ctx, logger)
		}
	}

	// Revocation endpoint must be HTTPS, or HTTP on loopback where a token
	// never crosses a network: tokens are sensitive credentials that must not
	// be transmitted in plaintext. An empty value stays legal.
	if v := conv.PtrValOr(payload.RevocationEndpoint, ""); v != "" && !urls.IsAbsoluteHTTPSOrLoopback(v) {
		return nil, oops.E(oops.CodeBadRequest, nil, "revocation_endpoint must be an absolute https URL, or http on loopback").LogError(ctx, logger)
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
	// issuers are edited via the organizationRemoteSessionIssuers service,
	// platform issuers only by platform admins, and the project-scoped
	// UpdateRemoteSessionIssuer below cannot modify either. Both inherited arms
	// stay off (IncludeOrganizational and IncludeGlobal false).
	existing, err := txRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:                    issuerID,
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeOrganizational: false,
		IncludeGlobal:         false,
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
		LogoAssetID:                       conv.PtrToPGText(payload.LogoAssetID),
		ClientSetupDocumentationUrl:       conv.PtrToPGText(payload.ClientSetupDocumentationURL),
		AuthorizationEndpoint:             conv.PtrToPGText(payload.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrToPGText(payload.TokenEndpoint),
		RevocationEndpoint:                conv.PtrToPGText(payload.RevocationEndpoint),
		RegistrationEndpoint:              conv.PtrToPGText(payload.RegistrationEndpoint),
		JwksUri:                           conv.PtrToPGText(payload.JwksURI),
		ServiceDocumentation:              conv.PtrToPGText(payload.ServiceDocumentation),
		OpPolicyUri:                       conv.PtrToPGText(payload.OpPolicyURI),
		OpTosUri:                          conv.PtrToPGText(payload.OpTosURI),
		ScopesSupported:                   payload.ScopesSupported,
		GrantTypesSupported:               payload.GrantTypesSupported,
		ResponseTypesSupported:            payload.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: payload.TokenEndpointAuthMethodsSupported,
		CodeChallengeMethodsSupported:     payload.CodeChallengeMethodsSupported,
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
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeOrganizational: true,
		IncludeGlobal:         true,
		Cursor:                cursor,
		LimitValue:            limit,
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

// GetRemoteSessionIssuer resolves a single issuer by id, slug, or upstream
// issuer URL. Exactly one of the three must be supplied.
//
// The not-found returns are logged at warn rather than error on purpose. A miss
// is a client-fault 404, and .LogError would also mark the OpenTelemetry span as
// errored — which matters most for the issuer arm, where a miss is the normal
// path: automatic setup asks "does an identity provider already describe this
// upstream?" and creates one when the answer is no. See AGE-3082 for the
// codebase-wide migration of the remaining CodeNotFound .LogError call sites.
func (s *Service) GetRemoteSessionIssuer(ctx context.Context, payload *gen.GetRemoteSessionIssuerPayload) (*types.RemoteSessionIssuer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	hasID := payload.ID != nil && *payload.ID != ""
	hasSlug := payload.Slug != nil && *payload.Slug != ""
	hasIssuer := payload.Issuer != nil && strings.TrimSpace(*payload.Issuer) != ""
	if conv.Ternary(hasID, 1, 0)+conv.Ternary(hasSlug, 1, 0)+conv.Ternary(hasIssuer, 1, 0) != 1 {
		return nil, oops.E(oops.CodeBadRequest, nil, "exactly one of id, slug, or issuer is required").LogError(ctx, logger)
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
			ID:                    issuerID,
			ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
			IncludeOrganizational: true,
			IncludeGlobal:         true,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogWarn(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
		}
	case hasIssuer:
		if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
			return nil, err
		}

		canonical, err := parseCanonicalIssuerURL(*payload.Issuer)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogError(ctx, logger)
		}

		// Both inherited tiers are in scope: an organization-level or platform
		// issuer describing this upstream is one the project may attach its own
		// client to, so it counts as found.
		candidates, err := repo.New(s.db).ListRemoteSessionIssuersByIssuerURL(ctx, repo.ListRemoteSessionIssuersByIssuerURLParams{
			Issuers:               canonical.matchCandidates(),
			ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			IncludeOrganizational: true,
			OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
			IncludeGlobal:         true,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list remote session issuers by issuer url").LogError(ctx, logger)
		}

		// Unlike id and slug, an issuer URL can match several rows: duplicates
		// across tiers are legitimate by design. Precedence decides which one the
		// project should use.
		match, found := resolveIssuerByPrecedence(candidates)
		if !found {
			return nil, oops.E(oops.CodeNotFound, nil, "remote session issuer not found").LogWarn(ctx, logger)
		}
		issuer = match
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
				return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogWarn(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
		}
	}

	return mv.BuildRemoteSessionIssuerView(issuer), nil
}

// GetRemoteSessionIssuerDuplicatePreflight reports the issuers this project can
// already see that describe a given upstream authorization server, so a create
// or edit form can warn before adding a second record for one issuer.
//
// Both inherited tiers are in scope, matching GetRemoteSessionIssuer's issuer
// arm: an organization-level or platform record describing this URL is one the
// project may attach its own client to instead. The project arm stays
// project_id = this project, so a sibling project's records never surface.
func (s *Service) GetRemoteSessionIssuerDuplicatePreflight(ctx context.Context, payload *gen.GetRemoteSessionIssuerDuplicatePreflightPayload) (*types.RemoteSessionIssuerDuplicatePreflight, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	canonical, err := parseCanonicalIssuerURL(conv.PtrValOrEmpty(payload.Issuer, ""))
	if err != nil {
		return emptyIssuerDuplicatePreflight(), nil
	}

	// Reuses the resolver's own query rather than a preflight-specific one, so
	// the two can never disagree about which records describe a URL. It carries
	// no LIMIT, because precedence resolution needs the whole candidate set;
	// buildIssuerDuplicatePreflight truncates the response instead.
	candidates, err := repo.New(s.db).ListRemoteSessionIssuersByIssuerURL(ctx, repo.ListRemoteSessionIssuersByIssuerURLParams{
		Issuers:               canonical.matchCandidates(),
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		IncludeOrganizational: true,
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:         true,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote session issuers by issuer url").LogError(ctx, logger)
	}

	// projectName stays empty at this tier: every project-specific match belongs
	// to the caller's own project, which the caller is already looking at.
	rows := make([]issuerDuplicateCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, issuerDuplicateCandidateFromRecord(candidate))
	}

	return buildIssuerDuplicatePreflight(rows), nil
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

	// Serialize the count-then-delete below against client creation: every
	// client writer takes this advisory lock before binding a client to the
	// issuer. Without it a create commits in the gap and strands a live client
	// on a deleted issuer, because the soft delete only rewrites deleted_at and
	// its FOR NO KEY UPDATE row lock does not conflict with the FOR KEY SHARE
	// the client insert's foreign key takes. Taking the advisory lock before any
	// row lock also matches the order the create paths use, so neither can
	// deadlock against the other.
	if err := txRepo.LockRemoteSessionIssuerForClientBinding(ctx, issuerID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock remote session issuer for client binding").LogError(ctx, logger)
	}

	// Establish the issuer belongs to the caller's project before counting
	// clients, so a foreign or platform issuer id returns NotFound. Without this
	// pre-read the unscoped count below runs first: a platform issuer that some
	// tenant has attached to would return a 409 (a cross-tenant existence oracle),
	// and one with no clients would fall through to the project-scoped delete,
	// match nothing, and return a silent success. Both inherited arms stay off: a
	// tenant must never delete an organization-level or platform issuer here.
	if _, err := txRepo.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:                    issuerID,
		ProjectID:             uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeOrganizational: false,
		IncludeGlobal:         false,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "remote session issuer not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get remote session issuer").LogError(ctx, logger)
	}

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
// It probes the well-known locations returned by IssuerMetadataProbeCandidates in
// order, returning the first that yields a usable document — one carrying both
// an authorization_endpoint and a token_endpoint. A 200 that parses but lacks
// those endpoints is almost always a SPA/gateway catch-all answering our
// speculative candidate rather than real metadata, so it is skipped in favor of
// a later candidate (e.g. the origin-style fallback); it is surfaced only as a
// last resort when no candidate yields a usable document. When every probe
// fails the first (canonical RFC 8414) candidate's error is surfaced, wrapped
// in a *discoveryError so the handler can attach the upstream URL and status to
// the user-facing error.
// DiscoveredIssuerMetadata is the server-owned subset of RFC 8414 metadata
// required to register Gram as an OAuth client. It is deliberately an internal
// application return type rather than an API payload: callers must not reflect
// upstream endpoints or registration material to untrusted clients.
type DiscoveredIssuerMetadata struct {
	Issuer                            string
	AuthorizationEndpoint             string
	TokenEndpoint                     string
	RegistrationEndpoint              string
	ScopesSupported                   []string
	GrantTypesSupported               []string
	ResponseTypesSupported            []string
	TokenEndpointAuthMethodsSupported []string

	// CodeChallengeMethodsSupported is never nil: discovery ran, so a document
	// that omits the field yields an empty slice — the persisted
	// "captured; the upstream advertises nothing" state — rather than the nil
	// that the nullable column would store as "never captured".
	CodeChallengeMethodsSupported []string

	ClientIDMetadataDocumentSupported bool

	// Metadata is the discovery document these fields were projected from,
	// filtered to what Gram is willing to re-publish, ready to store in
	// remote_session_issuers.metadata. Callers that persist an issuer from this
	// value should store it: they write the endpoints above from the same
	// document, so the snapshot cannot disagree with them, and it carries the
	// OIDC extension fields the typed columns do not model.
	//
	// Nil when the document could not be reduced to a storable snapshot, which
	// is not an error: the issuer simply reconstructs its well-known document
	// from the typed columns until a refresh captures one.
	Metadata []byte
}

// DiscoverIssuerMetadata performs issuer metadata discovery through Guardian's
// outbound policy. It is available to trusted server-side composition such as
// Platform MCP provider attachment; browser and MCP callers must never supply
// an issuer URL to it.
func DiscoverIssuerMetadata(ctx context.Context, policy *guardian.Policy, issuerURL string) (DiscoveredIssuerMetadata, error) {
	doc, raw, _, err := discoverIssuerMetadata(ctx, policy, issuerURL)
	if err != nil {
		return DiscoveredIssuerMetadata{}, err
	}
	// A document that cannot be reduced to a storable snapshot still yields
	// usable typed values, so the projection continues with a nil snapshot
	// rather than failing the caller's attachment over it.
	snapshot, _ := sanitizeDiscoverySnapshot(raw)
	return DiscoveredIssuerMetadata{
		Metadata: snapshot,

		Issuer:                            doc.Issuer,
		AuthorizationEndpoint:             doc.AuthorizationEndpoint,
		TokenEndpoint:                     doc.TokenEndpoint,
		RegistrationEndpoint:              doc.RegistrationEndpoint,
		ScopesSupported:                   append([]string(nil), doc.ScopesSupported...),
		GrantTypesSupported:               append([]string(nil), doc.GrantTypesSupported...),
		ResponseTypesSupported:            append([]string(nil), doc.ResponseTypesSupported...),
		TokenEndpointAuthMethodsSupported: append([]string(nil), doc.TokenEndpointAuthMethodsSupported...),

		// An empty advertised list must survive as empty here, because nil and
		// empty persist differently for this field (NULL "never captured" vs
		// {} "captured, advertises nothing"). The plain append copy used by
		// the sibling fields collapses an empty slice to nil, so it gets an
		// orEmptySlice on top.
		CodeChallengeMethodsSupported: orEmptySlice(append([]string(nil), doc.CodeChallengeMethodsSupported...)),

		ClientIDMetadataDocumentSupported: doc.ClientIDMetadataDocumentSupported,
	}, nil
}

// discoveredDocument pairs a parsed metadata document with the exact bytes it
// was parsed from.
//
// They travel as one value rather than as two variables because
// discoverIssuerMetadata may return a document remembered from an earlier probe
// candidate after later candidates fail. A `raw` tracked alongside the loop
// would then hold the last probe's body while the returned document came from
// an earlier one, and the snapshot persisted for the issuer would describe an
// authorization server Gram never adopted.
type discoveredDocument struct {
	doc rfc8414Document
	raw []byte
}

func discoverIssuerMetadata(ctx context.Context, policy *guardian.Policy, issuerURL string) (rfc8414Document, []byte, []string, error) {
	candidates, err := IssuerMetadataProbeCandidates(issuerURL)
	if err != nil {
		return rfc8414Document{}, nil, nil, &discoveryError{
			WellKnownURL: "",
			Status:       0,
			cause:        fmt.Errorf("compute well-known url: %w", err),
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, discoveryHTTPTimeout)
	defer cancel()

	client := policy.Client()
	// Guardian owns the transport and its SSRF protections. Keep redirect policy
	// narrow here without changing TLS verification or the transport itself.
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if !validIssuerDiscoveryURL(req.URL) {
			return errors.New("issuer discovery redirect target must use HTTPS outside local loopback")
		}
		return nil
	}

	var firstErr *discoveryError
	var fallback discoveredDocument
	haveFallback := false
	for _, wellKnown := range candidates {
		found, attemptErr := attemptIssuerProbe(reqCtx, client, wellKnown)
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
		if found.doc.AuthorizationEndpoint == "" || found.doc.TokenEndpoint == "" {
			if !haveFallback {
				fallback = found
				haveFallback = true
			}
			continue
		}

		return found.doc, found.raw, collectDiscoveryWarnings(issuerURL, found.doc), nil
	}

	if haveFallback {
		return fallback.doc, fallback.raw, collectDiscoveryWarnings(issuerURL, fallback.doc), nil
	}

	return rfc8414Document{}, nil, nil, firstErr
}

// attemptIssuerProbe issues a single GET against an issuer well-known URL and
// returns either the parsed RFC 8414 / OIDC document, paired with the bytes it
// was parsed from, or a typed error annotated with the probed URL and upstream
// status.
//
// The raw body is returned so callers can persist a snapshot of what the issuer
// actually advertised: the typed columns model only the fields Gram acts on, so
// re-serving from them would drop the OIDC extension fields an MCP client may
// rely on. It is only ever returned alongside a document that parsed and passed
// validateIssuerMetadataEndpoints, so a stored snapshot has cleared the same
// gate as the typed values beside it.
func attemptIssuerProbe(ctx context.Context, client *guardian.HTTPClient, wellKnown string) (discoveredDocument, *discoveryError) {
	requestURL, err := url.Parse(wellKnown)
	if err != nil || !validIssuerDiscoveryURL(requestURL) {
		return discoveredDocument{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       0,
			cause:        errors.New("issuer discovery URL must use HTTPS outside local loopback"),
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return discoveredDocument{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       0,
			cause:        fmt.Errorf("build discovery request: %w", err),
		}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return discoveredDocument{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       0,
			cause:        fmt.Errorf("fetch discovery document: %w", err),
		}
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return discoveredDocument{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       resp.StatusCode,
			cause:        fmt.Errorf("discovery returned status %d", resp.StatusCode),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return discoveredDocument{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       resp.StatusCode,
			cause:        fmt.Errorf("read discovery body: %w", err),
		}
	}

	var doc rfc8414Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return discoveredDocument{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       resp.StatusCode,
			cause:        fmt.Errorf("decode discovery document: %w", err),
		}
	}
	if err := validateIssuerMetadataEndpoints(doc, requestURL); err != nil {
		return discoveredDocument{}, &discoveryError{
			WellKnownURL: wellKnown,
			Status:       resp.StatusCode,
			cause:        err,
		}
	}

	return discoveredDocument{doc: doc, raw: body}, nil
}

// IssuerMetadataProbeCandidates returns the ordered list of well-known metadata URLs to
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
func IssuerMetadataProbeCandidates(issuerURL string) ([]string, error) {
	u, err := url.Parse(issuerURL)
	if err != nil {
		return nil, fmt.Errorf("parse issuer url: %w", err)
	}
	if !validIssuerDiscoveryURL(u) {
		return nil, fmt.Errorf("issuer url must use HTTPS outside local loopback")
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

// validIssuerDiscoveryURL permits HTTPS issuers and the explicit HTTP loopback
// exception used by local development and deterministic tests. It is applied to
// every initial probe and redirect before a request leaves the process.
func validIssuerDiscoveryURL(u *url.URL) bool {
	if u == nil || u.Host == "" || u.User != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateIssuerMetadataEndpoints rejects endpoints that would weaken the
// transport guarantee after a valid metadata document has been discovered.
// Authorization and token endpoints retain the explicit local-loopback
// exception used by discovery. JWKs and DCR endpoints are fetched server-side,
// so they require HTTPS except for an endpoint on the exact same explicit
// loopback origin as a local HTTP issuer.
func validateIssuerMetadataEndpoints(doc rfc8414Document, requestedIssuer *url.URL) error {
	for _, endpoint := range []struct {
		name         string
		raw          string
		requireHTTPS bool
	}{
		{name: "authorization_endpoint", raw: doc.AuthorizationEndpoint, requireHTTPS: false},
		{name: "token_endpoint", raw: doc.TokenEndpoint, requireHTTPS: true},
		{name: "jwks_uri", raw: doc.JwksURI, requireHTTPS: true},
		{name: "registration_endpoint", raw: doc.RegistrationEndpoint, requireHTTPS: true},
	} {
		if endpoint.raw == "" {
			continue
		}
		parsed, err := url.Parse(endpoint.raw)
		if err != nil || !validIssuerMetadataEndpointURL(parsed, requestedIssuer, endpoint.requireHTTPS) {
			if endpoint.requireHTTPS {
				return fmt.Errorf("issuer metadata %s must use HTTPS or the same local loopback origin", endpoint.name)
			}
			return fmt.Errorf("issuer metadata %s must use HTTPS outside local loopback", endpoint.name)
		}
	}
	return nil
}

func validIssuerMetadataEndpointURL(parsed, requestedIssuer *url.URL, requireHTTPS bool) bool {
	if parsed == nil || !parsed.IsAbs() || parsed.User != nil || parsed.Host == "" {
		return false
	}
	if !requireHTTPS {
		return validIssuerDiscoveryURL(parsed)
	}
	if parsed.Scheme == "https" {
		return true
	}
	return parsed.Scheme == "http" && validIssuerDiscoveryURL(parsed) && validIssuerDiscoveryURL(requestedIssuer) && parsed.Host == requestedIssuer.Host
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
	// Advisory rather than a defect report: RFC 8414 makes the field OPTIONAL,
	// but MCP requires clients to refuse authorization servers that do not
	// advertise PKCE support, so a future change may enforce it. The
	// absent and empty cases read differently because only the wording here
	// can distinguish them — persistence collapses both to an empty array.
	switch {
	case doc.CodeChallengeMethodsSupported == nil:
		warnings = append(warnings, "code_challenge_methods_supported missing from discovery document; the MCP specification requires verifying that the identity provider advertises PKCE S256 support, and a future change may enforce this")
	case !slices.Contains(doc.CodeChallengeMethodsSupported, "S256"):
		warnings = append(warnings, "discovery document does not list S256 in code_challenge_methods_supported; the MCP specification requires verifying that the identity provider advertises PKCE S256 support, and a future change may enforce this")
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
