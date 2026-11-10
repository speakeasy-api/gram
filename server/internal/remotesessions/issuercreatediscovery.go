package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// defaultCreateDiscoveryBudget bounds the discovery and key-set round trips a create waits on.
const defaultCreateDiscoveryBudget = 5 * time.Second

// recordCreateDiscovery re-runs discovery for a create built from a discovered
// draft and, when the served document still describes the submitted form,
// records the row as discovered with the document's grant profiles.
//
// authorization_grant_profiles_supported appears only in discovered drafts, so
// a form that forwards it (even as an empty array) marks the create as
// discovery-built; creates that omit it skip the round trip. A submitted
// value that differs from the document leaves the row undiscovered, as do a
// discovery failure and an invalid key set: the create still succeeds and the
// on-use refresh captures the metadata later. An unstamped row never keeps the
// submitted grant profiles, since nothing vetted them. Columns the form omitted
// are filled from the document, as the on-use reprojection would.
func (s *Service) recordCreateDiscovery(ctx context.Context, logger *slog.Logger, tunnelID uuid.NullUUID, params *repo.CreateRemoteSessionIssuerParams, now time.Time) {
	if params.AuthorizationGrantProfilesSupported == nil {
		return
	}
	stamped := false
	defer func() {
		if !stamped {
			params.AuthorizationGrantProfilesSupported = []string{}
		}
	}()
	logger = logger.With(attr.SlogOAuthIssuer(params.Issuer))
	ctx, cancel := context.WithTimeout(ctx, s.createDiscoveryBudget)
	defer cancel()

	tunnel, err := issuerTunnelTransport(s.tunnels, tunnelID)
	if err != nil {
		logger.WarnContext(ctx, "select create-time issuer discovery transport", attr.SlogError(err))
		return
	}
	var doer httpDoer = issuerDiscoveryHTTPClient(s.policy)
	if tunnel != nil {
		doer = tunnel
	}

	discovered, err := discoverIssuerMetadataWithDoer(ctx, doer, params.Issuer)
	if err != nil {
		logger.InfoContext(ctx, "create-time issuer discovery failed; creating undiscovered", attr.SlogError(err))
		return
	}
	if discovered.unreadableErr != nil {
		logger.InfoContext(ctx, "create-time issuer discovery was incomplete; creating undiscovered", attr.SlogError(discovered.unreadableErr))
		return
	}
	if discovered.originFallback {
		logger.InfoContext(ctx, "create-time issuer discovery used an origin-root document; creating undiscovered")
		return
	}
	doc := discovered.doc
	if err := vetDiscoveredDocument(doc, params.Issuer); err != nil {
		logger.InfoContext(ctx, "create-time issuer discovery returned an untrusted document; creating undiscovered", attr.SlogError(err))
		return
	}
	if field := createFormDocumentMismatch(params, doc); field != "" {
		logger.InfoContext(ctx, "create-time issuer discovery skipped: submitted form differs from the served document", attr.SlogReason(field))
		return
	}
	// Same key-set gate as refresh; the set itself is fetched again on first use.
	if _, err := refreshIssuerKeySet(ctx, s.jwksResolver, tunnel, doc.JwksURI, repo.RemoteSessionIssuer{}); err != nil { //nolint:exhaustruct // No stored key set to reuse on create.
		logger.InfoContext(ctx, "create-time issuer key set validation failed; creating undiscovered", attr.SlogError(err))
		return
	}

	stamped = true
	params.Metadata = retainableDocument(doc.raw)
	params.MetadataFetchedAt = conv.ToPGTimestamptz(now)
	params.AuthorizationGrantProfilesSupported = orEmptySlice(doc.AuthorizationGrantProfilesSupported)
	params.AuthorizationEndpoint = projectOmittedText(params.AuthorizationEndpoint, doc.AuthorizationEndpoint)
	params.TokenEndpoint = projectOmittedText(params.TokenEndpoint, doc.TokenEndpoint)
	params.RevocationEndpoint = projectOmittedText(params.RevocationEndpoint, doc.RevocationEndpoint)
	params.RegistrationEndpoint = projectOmittedText(params.RegistrationEndpoint, doc.RegistrationEndpoint)
	params.JwksUri = projectOmittedText(params.JwksUri, doc.JwksURI)
	params.UserinfoEndpoint = projectOmittedText(params.UserinfoEndpoint, doc.UserinfoEndpoint)
	params.IntrospectionEndpoint = projectOmittedText(params.IntrospectionEndpoint, doc.IntrospectionEndpoint)
	params.ServiceDocumentation = projectOmittedText(params.ServiceDocumentation, doc.ServiceDocumentation)
	params.OpPolicyUri = projectOmittedText(params.OpPolicyUri, doc.OpPolicyURI)
	params.OpTosUri = projectOmittedText(params.OpTosUri, doc.OpTosURI)
	params.ScopesSupported = projectOmittedSlice(params.ScopesSupported, doc.ScopesSupported)
	params.GrantTypesSupported = projectOmittedSlice(params.GrantTypesSupported, doc.GrantTypesSupported)
	params.ResponseTypesSupported = projectOmittedSlice(params.ResponseTypesSupported, doc.ResponseTypesSupported)
	params.TokenEndpointAuthMethodsSupported = projectOmittedSlice(params.TokenEndpointAuthMethodsSupported, doc.TokenEndpointAuthMethodsSupported)
	params.CodeChallengeMethodsSupported = projectOmittedSlice(params.CodeChallengeMethodsSupported, doc.CodeChallengeMethodsSupported)
	params.IntrospectionEndpointAuthMethodsSupported = projectOmittedSlice(params.IntrospectionEndpointAuthMethodsSupported, doc.IntrospectionEndpointAuthMethodsSupported)
	params.IDTokenSigningAlgValuesSupported = projectOmittedSlice(params.IDTokenSigningAlgValuesSupported, doc.IDTokenSigningAlgValuesSupported)
	params.ClaimsSupported = projectOmittedSlice(params.ClaimsSupported, doc.ClaimsSupported)
	params.ClientIDMetadataDocumentSupported = params.ClientIDMetadataDocumentSupported || doc.ClientIDMetadataDocumentSupported
	params.BackchannelLogoutSupported = projectOmittedBool(params.BackchannelLogoutSupported, doc.BackchannelLogoutSupported)
	params.AuthorizationResponseIssParameterSupported = projectOmittedBool(params.AuthorizationResponseIssParameterSupported, doc.AuthorizationResponseIssParameterSupported)
}

// preflightCreateDiscoverySlug refuses a taken project slug before the outbound
// discovery call; the insert still enforces it. Organization-level slugs carry
// no uniqueness constraint, so only project-owned creates are checked.
func (s *Service) preflightCreateDiscoverySlug(ctx context.Context, logger *slog.Logger, params *repo.CreateRemoteSessionIssuerParams) error {
	if params.AuthorizationGrantProfilesSupported == nil || !params.ProjectID.Valid {
		return nil
	}
	_, err := repo.New(s.db).GetRemoteSessionIssuerBySlug(ctx, repo.GetRemoteSessionIssuerBySlugParams{Slug: params.Slug, ProjectID: params.ProjectID})
	switch {
	case err == nil:
		return oops.E(oops.CodeConflict, nil, "an issuer with this slug already exists").LogError(ctx, logger)
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	default:
		return oops.E(oops.CodeUnexpected, err, "check remote session issuer slug").LogError(ctx, logger)
	}
}

// createFormDocumentMismatch names the first submitted column that disagrees
// with doc, or "" when none does. Omitted (empty or false) values never
// disagree; they are filled from doc instead. oidc and passthrough are
// Speakeasy behavior flags, not document members.
func createFormDocumentMismatch(params *repo.CreateRemoteSessionIssuerParams, doc rfc8414Document) string {
	texts := []struct {
		field     string
		submitted pgtype.Text
		served    string
	}{
		{"authorization_endpoint", params.AuthorizationEndpoint, doc.AuthorizationEndpoint},
		{"token_endpoint", params.TokenEndpoint, doc.TokenEndpoint},
		{"revocation_endpoint", params.RevocationEndpoint, doc.RevocationEndpoint},
		{"registration_endpoint", params.RegistrationEndpoint, doc.RegistrationEndpoint},
		{"jwks_uri", params.JwksUri, doc.JwksURI},
		{"userinfo_endpoint", params.UserinfoEndpoint, doc.UserinfoEndpoint},
		{"introspection_endpoint", params.IntrospectionEndpoint, doc.IntrospectionEndpoint},
		{"service_documentation", params.ServiceDocumentation, doc.ServiceDocumentation},
		{"op_policy_uri", params.OpPolicyUri, doc.OpPolicyURI},
		{"op_tos_uri", params.OpTosUri, doc.OpTosURI},
	}
	for _, t := range texts {
		if v := conv.FromPGTextOrEmpty[string](t.submitted); v != "" && v != t.served {
			return t.field
		}
	}

	lists := []struct {
		field     string
		submitted []string
		served    []string
	}{
		{"scopes_supported", params.ScopesSupported, doc.ScopesSupported},
		{"grant_types_supported", params.GrantTypesSupported, doc.GrantTypesSupported},
		{"response_types_supported", params.ResponseTypesSupported, doc.ResponseTypesSupported},
		{"token_endpoint_auth_methods_supported", params.TokenEndpointAuthMethodsSupported, doc.TokenEndpointAuthMethodsSupported},
		{"code_challenge_methods_supported", params.CodeChallengeMethodsSupported, doc.CodeChallengeMethodsSupported},
		{"introspection_endpoint_auth_methods_supported", params.IntrospectionEndpointAuthMethodsSupported, doc.IntrospectionEndpointAuthMethodsSupported},
		{"id_token_signing_alg_values_supported", params.IDTokenSigningAlgValuesSupported, doc.IDTokenSigningAlgValuesSupported},
		{"claims_supported", params.ClaimsSupported, doc.ClaimsSupported},
	}
	for _, l := range lists {
		if len(l.submitted) != 0 && !sameStringSet(l.submitted, l.served) {
			return l.field
		}
	}

	switch {
	case params.ClientIDMetadataDocumentSupported && !doc.ClientIDMetadataDocumentSupported:
		return "client_id_metadata_document_supported"
	case params.BackchannelLogoutSupported.Bool && !doc.BackchannelLogoutSupported:
		return "backchannel_logout_supported"
	case params.AuthorizationResponseIssParameterSupported.Bool && !doc.AuthorizationResponseIssParameterSupported:
		return "authorization_response_iss_parameter_supported"
	default:
		return ""
	}
}

func sameStringSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

func projectOmittedText(submitted pgtype.Text, discovered string) pgtype.Text {
	if conv.FromPGTextOrEmpty[string](submitted) != "" {
		return submitted
	}
	return conv.ToPGTextEmpty(discovered)
}

func projectOmittedSlice(submitted, discovered []string) []string {
	if len(submitted) != 0 {
		return submitted
	}
	return orEmptySlice(discovered)
}

func projectOmittedBool(submitted pgtype.Bool, discovered bool) pgtype.Bool {
	if submitted.Bool {
		return submitted
	}
	return pgtype.Bool{Bool: discovered, Valid: true}
}
