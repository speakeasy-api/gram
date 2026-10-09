package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	issuersgen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	rsgen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

type createDiscoveryUpstream struct {
	server     *httptest.Server
	requests   atomic.Int32
	fail       atomic.Bool
	unsafeKeys atomic.Bool
}

var (
	servedScopes         = []string{"openid"}
	servedGrantTypes     = []string{"authorization_code", oauthwire.GrantTypeJWTBearer}
	servedResponseTypes  = []string{"code"}
	servedTokenEndpoints = []string{"client_secret_basic"}
)

func newCreateDiscoveryUpstream(t *testing.T) *createDiscoveryUpstream {
	t.Helper()
	u := &createDiscoveryUpstream{server: nil, requests: atomic.Int32{}, fail: atomic.Bool{}, unsafeKeys: atomic.Bool{}}
	u.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.requests.Add(1)
		if u.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                                u.server.URL,
				"authorization_endpoint":                u.server.URL + "/authorize",
				"token_endpoint":                        u.server.URL + "/token",
				"registration_endpoint":                 u.server.URL + "/register",
				"jwks_uri":                              u.server.URL + "/jwks",
				"scopes_supported":                      servedScopes,
				"grant_types_supported":                 servedGrantTypes,
				"response_types_supported":              servedResponseTypes,
				"token_endpoint_auth_methods_supported": servedTokenEndpoints,
				oauthwire.MetadataAuthorizationGrantProfilesSupported: []string{oauthwire.GrantProfileIDJAG},
			})
		case "/jwks":
			w.Header().Set("Content-Type", "application/jwk-set+json")
			if u.unsafeKeys.Load() {
				_, _ = w.Write([]byte(`{"keys":[{"kty":"oct","kid":"unsafe","k":"c2VjcmV0"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"keys":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(u.server.Close)
	return u
}

// discoveredDraftPayload is a project-tier create whose form matches what upstream serves.
func discoveredDraftPayload(slug string, upstream *createDiscoveryUpstream) *issuersgen.CreateRemoteSessionIssuerPayload {
	payload := newIssuerPayloadForURL(slug, upstream.server.URL)
	payload.AuthorizationEndpoint = conv.PtrEmpty(upstream.server.URL + "/authorize")
	payload.TokenEndpoint = conv.PtrEmpty(upstream.server.URL + "/token")
	payload.RegistrationEndpoint = conv.PtrEmpty(upstream.server.URL + "/register")
	payload.JwksURI = conv.PtrEmpty(upstream.server.URL + "/jwks")
	payload.ScopesSupported = servedScopes
	payload.GrantTypesSupported = servedGrantTypes
	payload.ResponseTypesSupported = servedResponseTypes
	payload.TokenEndpointAuthMethodsSupported = servedTokenEndpoints
	payload.AuthorizationGrantProfilesSupported = []string{}
	return payload
}

// discoveredOrgDraftPayload is discoveredDraftPayload for the organization tier.
func discoveredOrgDraftPayload(slug string, projectID *string, upstream *createDiscoveryUpstream) *orgissuersgen.CreateIssuerPayload {
	payload := newCreateIssuerPayload(slug, projectID)
	payload.Issuer = upstream.server.URL
	payload.AuthorizationEndpoint = conv.PtrEmpty(upstream.server.URL + "/authorize")
	payload.TokenEndpoint = conv.PtrEmpty(upstream.server.URL + "/token")
	payload.ScopesSupported = servedScopes
	payload.GrantTypesSupported = servedGrantTypes
	payload.ResponseTypesSupported = servedResponseTypes
	payload.TokenEndpointAuthMethodsSupported = servedTokenEndpoints
	payload.AuthorizationGrantProfilesSupported = []string{}
	return payload
}

// discoveredProviderForm is a CommitServerIdentityConfiguration provider form matching upstream.
func discoveredProviderForm(slug string, upstream *createDiscoveryUpstream) *rsgen.CreateRemoteSessionIssuerForm {
	form := serverIdentityProviderForm(slug, nil, false)
	form.Issuer = upstream.server.URL
	form.AuthorizationEndpoint = conv.PtrEmpty(upstream.server.URL + "/authorize")
	form.TokenEndpoint = conv.PtrEmpty(upstream.server.URL + "/token")
	form.ScopesSupported = servedScopes
	form.GrantTypesSupported = servedGrantTypes
	form.ResponseTypesSupported = servedResponseTypes
	form.TokenEndpointAuthMethodsSupported = servedTokenEndpoints
	form.AuthorizationGrantProfilesSupported = []string{}
	form.CodeChallengeMethodsSupported = nil
	return form
}

func commitDiscoveredProvider(t *testing.T, ctx context.Context, ti *testInstance, form *rsgen.CreateRemoteSessionIssuerForm) *repo.RemoteSessionIssuer {
	t.Helper()
	targetID, _ := createServerIdentityTarget(t, ctx, ti, form.Slug+"-target")
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, &rsgen.CommitServerIdentityConfigurationPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		McpServerID:        targetID.String(),
		ProviderID:         nil,
		CreateProvider:     form,
		ClientMode:         "manual",
		RegistrationMethod: nil,
		ExistingClientID:   nil,
		ClientConfiguration: &rsgen.ServerIdentityClientConfiguration{
			ClientID:                conv.PtrEmpty("discovered-client"),
			ClientSecret:            conv.PtrEmpty("discovered-secret"),
			TokenEndpointAuthMethod: conv.PtrEmpty("client_secret_basic"),
			Scope:                   []string{"openid"},
			Audience:                nil,
		},
	})
	require.NoError(t, err)
	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(result.Provider.ID))
	return &row
}

func TestCreateRemoteSessionIssuer_DiscoveredDraftRecordsServerDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-discovered", upstream)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.True(t, row.MetadataFetchedAt.Valid, "a create built from discovery must record that the server discovered it")
	require.Equal(t, []string{oauthwire.GrantProfileIDJAG}, row.AuthorizationGrantProfilesSupported, "the server's discovered profiles win over the submitted ones")
	require.NotEmpty(t, row.Metadata)
	require.Positive(t, upstream.requests.Load())
	require.True(t, row.AuthorizationResponseIssParameterSupported.Valid, "omitted capability columns are projected from the document")
	require.False(t, row.AuthorizationResponseIssParameterSupported.Bool)
}

func TestCreateRemoteSessionIssuer_MismatchedJwksURIIsNotRecordedAsDiscovered(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-jwks-mismatch", upstream)
	payload.JwksURI = conv.PtrEmpty(upstream.server.URL + "/other-jwks")

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid, "a submitted jwks_uri the document does not serve is not that document")
	require.Empty(t, row.Metadata)
}

func TestCreateRemoteSessionIssuer_MismatchedIssFlagIsNotRecordedAsDiscovered(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-iss-mismatch", upstream)
	payload.AuthorizationResponseIssParameterSupported = conv.PtrEmpty(true)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid)
	require.True(t, row.AuthorizationResponseIssParameterSupported.Bool, "the submitted flag is kept when discovery is not recorded")
}

func TestCreateRemoteSessionIssuer_TakenSlugSkipsDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	first := discoveredDraftPayload("create-taken-slug", upstream)
	first.AuthorizationGrantProfilesSupported = nil
	_, err := ti.service.CreateRemoteSessionIssuer(ctx, first)
	require.NoError(t, err)

	_, err = ti.service.CreateRemoteSessionIssuer(ctx, discoveredDraftPayload("create-taken-slug", upstream))
	requireOopsCode(t, err, oops.CodeConflict)
	require.Zero(t, upstream.requests.Load(), "a create refused on its slug must not reach the upstream")
}

func TestCreateRemoteSessionIssuer_HandTypedSkipsDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-hand-typed", upstream)
	payload.AuthorizationGrantProfilesSupported = nil

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Zero(t, upstream.requests.Load(), "a hand-typed create must not reach the upstream")
}

func TestCreateRemoteSessionIssuer_EditedEndpointsAreNotRecordedAsDiscovered(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := newIssuerPayloadForURL("create-edited", upstream.server.URL)
	payload.AuthorizationGrantProfilesSupported = []string{}

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid, "a row whose endpoints differ from the served document is not that document")
	require.Empty(t, row.AuthorizationGrantProfilesSupported)
}

func TestCreateRemoteSessionIssuer_MismatchedRegistrationEndpointIsNotRecordedAsDiscovered(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-registration-mismatch", upstream)
	payload.RegistrationEndpoint = conv.PtrEmpty("https://stale.example.com/register")

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Equal(t, "https://stale.example.com/register", row.RegistrationEndpoint.String)
}

func TestCreateRemoteSessionIssuer_MismatchedGrantTypesAreNotRecordedAsDiscovered(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-grant-types-mismatch", upstream)
	payload.GrantTypesSupported = []string{"authorization_code"}

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Empty(t, row.AuthorizationGrantProfilesSupported)
}

func TestCreateRemoteSessionIssuer_OmittedFieldsAreFilledFromDocument(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-omitted-filled", upstream)
	payload.RegistrationEndpoint = nil
	payload.JwksURI = nil
	payload.GrantTypesSupported = nil

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.True(t, row.MetadataFetchedAt.Valid)
	require.Equal(t, upstream.server.URL+"/register", row.RegistrationEndpoint.String)
	require.Equal(t, upstream.server.URL+"/jwks", row.JwksUri.String)
	require.Equal(t, servedGrantTypes, row.GrantTypesSupported)
}

func TestCreateRemoteSessionIssuer_InvalidKeySetIsNotRecordedAsDiscovered(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	upstream.unsafeKeys.Store(true)
	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, discoveredDraftPayload("create-unsafe-keys", upstream))
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid, "a key set refresh would refuse is not recorded as discovered")
	require.Empty(t, row.AuthorizationGrantProfilesSupported)
}

func TestCreateRemoteSessionIssuer_DiscoveryFailureStillCreates(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	upstream.fail.Store(true)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-discovery-fails", upstream)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Positive(t, upstream.requests.Load())
}

func TestCreateIssuer_DiscoveredDraftRecordsServerDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	created, err := ti.service.CreateIssuer(ctx, discoveredOrgDraftPayload("org-create-discovered", nil, upstream))
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.True(t, row.MetadataFetchedAt.Valid)
	require.Equal(t, []string{oauthwire.GrantProfileIDJAG}, row.AuthorizationGrantProfilesSupported)
}

func TestCreateIssuer_HandTypedSkipsDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredOrgDraftPayload("org-create-hand-typed", nil, upstream)
	payload.AuthorizationGrantProfilesSupported = nil

	created, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Zero(t, upstream.requests.Load(), "a hand-typed create must not reach the upstream")
}

func TestCreateIssuer_TakenProjectSlugSkipsDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)
	projectID := projectIDFromContext(t, ctx).String()

	first := discoveredOrgDraftPayload("org-create-taken-slug", &projectID, upstream)
	first.AuthorizationGrantProfilesSupported = nil
	_, err := ti.service.CreateIssuer(ctx, first)
	require.NoError(t, err)

	_, err = ti.service.CreateIssuer(ctx, discoveredOrgDraftPayload("org-create-taken-slug", &projectID, upstream))
	requireOopsCode(t, err, oops.CodeConflict)
	require.Zero(t, upstream.requests.Load(), "a create refused on its slug must not reach the upstream")
}

func TestCommitServerIdentityConfiguration_CreatedProviderRecordsServerDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	row := commitDiscoveredProvider(t, ctx, ti, discoveredProviderForm("discovered-provider", upstream))
	require.True(t, row.MetadataFetchedAt.Valid, "a provider created from discovered metadata must be recorded as discovered")
	require.Equal(t, []string{oauthwire.GrantProfileIDJAG}, row.AuthorizationGrantProfilesSupported)
}

func TestCommitServerIdentityConfiguration_HandTypedProviderSkipsDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	form := discoveredProviderForm("hand-typed-provider", upstream)
	form.AuthorizationGrantProfilesSupported = nil

	row := commitDiscoveredProvider(t, ctx, ti, form)
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Zero(t, upstream.requests.Load(), "a hand-typed provider must not reach the upstream")
}

func TestCreateGlobalIssuer_DiscoveredDraftRecordsServerDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	payload := createGlobalIssuer(t, "global-create-discovered")
	payload.Issuer = upstream.server.URL
	payload.AuthorizationEndpoint = conv.PtrEmpty(upstream.server.URL + "/authorize")
	payload.TokenEndpoint = conv.PtrEmpty(upstream.server.URL + "/token")
	payload.GrantTypesSupported = servedGrantTypes
	payload.AuthorizationGrantProfilesSupported = []string{}

	created, err := ti.service.CreateGlobalIssuer(adminCtx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.True(t, row.MetadataFetchedAt.Valid)
	require.Equal(t, []string{oauthwire.GrantProfileIDJAG}, row.AuthorizationGrantProfilesSupported)
	require.Equal(t, servedScopes, row.ScopesSupported, "omitted lists are filled from the document")
}

func TestCreateGlobalIssuer_HandTypedSkipsDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	payload := createGlobalIssuer(t, "global-create-hand-typed")
	payload.Issuer = upstream.server.URL

	created, err := ti.service.CreateGlobalIssuer(adminCtx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid)
	require.Empty(t, row.AuthorizationGrantProfilesSupported)
	require.Zero(t, upstream.requests.Load())
}

func TestCreateRemoteSessionIssuer_UnvettedProfilesAreDropped(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(*createDiscoveryUpstream, *issuersgen.CreateRemoteSessionIssuerPayload)
	}{
		{"discovery failure", func(u *createDiscoveryUpstream, _ *issuersgen.CreateRemoteSessionIssuerPayload) { u.fail.Store(true) }},
		{"form mismatch", func(u *createDiscoveryUpstream, p *issuersgen.CreateRemoteSessionIssuerPayload) {
			p.JwksURI = conv.PtrEmpty(u.server.URL + "/other-jwks")
		}},
		{"invalid key set", func(u *createDiscoveryUpstream, _ *issuersgen.CreateRemoteSessionIssuerPayload) {
			u.unsafeKeys.Store(true)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			upstream := newCreateDiscoveryUpstream(t)
			ctx, ti := newTestService(t)
			payload := discoveredDraftPayload("create-unvetted-"+uuid.NewString()[:8], upstream)
			payload.AuthorizationGrantProfilesSupported = []string{oauthwire.GrantProfileIDJAG}
			tc.setup(upstream, payload)

			created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
			require.NoError(t, err)

			row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
			require.False(t, row.MetadataFetchedAt.Valid)
			require.Empty(t, row.AuthorizationGrantProfilesSupported, "submitted profiles no discovery vetted must not persist")
		})
	}
}

func TestCreateRemoteSessionIssuer_ReorderedListsAreStillDiscovered(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	payload := discoveredDraftPayload("create-reordered", upstream)
	payload.GrantTypesSupported = slices.Clone(servedGrantTypes)
	slices.Reverse(payload.GrantTypesSupported)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.True(t, row.MetadataFetchedAt.Valid, "supported lists are sets; order is not a mismatch")
}

func TestCreateRemoteSessionIssuer_OriginFallbackDocumentIsNotRecordedAsDiscovered(t *testing.T) {
	t.Parallel()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 server.URL + "/tenant",
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				oauthwire.MetadataAuthorizationGrantProfilesSupported: []string{oauthwire.GrantProfileIDJAG},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	ctx, ti := newTestService(t)

	payload := newIssuerPayloadForURL("create-origin-fallback", server.URL+"/tenant")
	payload.AuthorizationEndpoint = conv.PtrEmpty(server.URL + "/authorize")
	payload.TokenEndpoint = conv.PtrEmpty(server.URL + "/token")
	payload.RegistrationEndpoint = nil
	payload.JwksURI = nil
	payload.ScopesSupported = nil
	payload.GrantTypesSupported = nil
	payload.ResponseTypesSupported = nil
	payload.TokenEndpointAuthMethodsSupported = nil
	payload.AuthorizationGrantProfilesSupported = []string{}

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid, "an origin-root document may describe the whole host, not this issuer")
	require.Empty(t, row.AuthorizationGrantProfilesSupported)
}

func TestCreateRemoteSessionIssuer_RedundantOriginDocumentKeepsPathDiscovery(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		originExtra map[string]any
		stamped     bool
	}{
		"redundant origin document": {originExtra: map[string]any{}, stamped: true},
		"origin adds a member":      {originExtra: map[string]any{"claims_supported": []string{"sub"}}, stamped: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				issuer := server.URL + "/tenant"
				switch r.URL.Path {
				case "/.well-known/oauth-authorization-server/tenant":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"issuer":                 issuer,
						"authorization_endpoint": server.URL + "/authorize",
						"token_endpoint":         server.URL + "/token",
						oauthwire.MetadataAuthorizationGrantProfilesSupported: []string{oauthwire.GrantProfileIDJAG},
					})
				case "/.well-known/openid-configuration":
					doc := map[string]any{"issuer": issuer, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token"}
					for k, v := range tc.originExtra {
						doc[k] = v
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(doc)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			ctx, ti := newTestService(t)

			payload := newIssuerPayloadForURL("create-redundant-origin", server.URL+"/tenant")
			payload.AuthorizationEndpoint = conv.PtrEmpty(server.URL + "/authorize")
			payload.TokenEndpoint = conv.PtrEmpty(server.URL + "/token")
			payload.RegistrationEndpoint = nil
			payload.JwksURI = nil
			payload.ScopesSupported = nil
			payload.GrantTypesSupported = nil
			payload.ResponseTypesSupported = nil
			payload.TokenEndpointAuthMethodsSupported = nil
			payload.AuthorizationGrantProfilesSupported = []string{}

			created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
			require.NoError(t, err)

			row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
			require.Equal(t, tc.stamped, row.MetadataFetchedAt.Valid, "only an origin document that contributes members taints path discovery")
		})
	}
}

func TestCreateRemoteSessionIssuer_DiscoveryIsTimeBoxed(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	ctx, ti := newTestService(t)
	ti.service.SetCreateDiscoveryBudget(100 * time.Millisecond)

	payload := newIssuerPayloadForURL("create-slow-discovery", server.URL)
	payload.AuthorizationGrantProfilesSupported = []string{}

	start := time.Now()
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.Less(t, time.Since(start), 3*time.Second, "create must stop waiting once the discovery budget is spent")

	row := loadIssuerByID(t, ctx, ti, uuid.MustParse(created.ID))
	require.False(t, row.MetadataFetchedAt.Valid)
}

func TestCommitServerIdentityConfiguration_RegistrationFollowsSubmittedForm(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	form := discoveredProviderForm("pinned-registration", upstream)
	targetID, _ := createServerIdentityTarget(t, ctx, ti, form.Slug+"-target")
	result, err := ti.service.CommitServerIdentityConfiguration(ctx, &rsgen.CommitServerIdentityConfigurationPayload{
		McpServerID:    targetID.String(),
		CreateProvider: form,
		ClientMode:     "auto",
		ClientConfiguration: &rsgen.ServerIdentityClientConfiguration{
			ClientID:                nil,
			ClientSecret:            nil,
			TokenEndpointAuthMethod: nil,
			Scope:                   nil,
			Audience:                nil,
		},
	})
	require.NoError(t, err)
	require.True(t, result.ManualSetupRequired, "a registration endpoint discovery filled in must not switch the submitted form to dynamic registration")
}

func TestUpdateIssuer_NewIssuerURLDiscardsDiscovery(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, discoveredDraftPayload("update-moved", upstream))
	require.NoError(t, err)
	id := uuid.MustParse(created.ID)
	require.True(t, loadIssuerByID(t, ctx, ti, id).MetadataFetchedAt.Valid)

	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &issuersgen.UpdateRemoteSessionIssuerPayload{ID: created.ID, Issuer: conv.PtrEmpty(upstream.server.URL)})
	require.NoError(t, err)
	kept := loadIssuerByID(t, ctx, ti, id)
	require.True(t, kept.MetadataFetchedAt.Valid, "restating the same issuer keeps discovery")
	require.Equal(t, []string{oauthwire.GrantProfileIDJAG}, kept.AuthorizationGrantProfilesSupported)

	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &issuersgen.UpdateRemoteSessionIssuerPayload{
		ID:                                  created.ID,
		Issuer:                              conv.PtrEmpty("https://moved.example.com"),
		AuthorizationGrantProfilesSupported: []string{oauthwire.GrantProfileIDJAG},
	})
	require.NoError(t, err)
	moved := loadIssuerByID(t, ctx, ti, id)
	require.False(t, moved.MetadataFetchedAt.Valid)
	require.Empty(t, moved.Metadata)
	require.Empty(t, moved.AuthorizationGrantProfilesSupported, "profiles from the old issuer or the form are unvetted for the new one")
	require.False(t, moved.AuthorizationEndpoint.Valid, "the old issuer's endpoints must not survive a move")
	require.False(t, moved.TokenEndpoint.Valid)
	require.False(t, moved.RegistrationEndpoint.Valid)
	require.False(t, moved.JwksUri.Valid)
	require.Empty(t, moved.GrantTypesSupported)
	require.Empty(t, moved.TokenEndpointAuthMethodsSupported)
}

func TestUpdateIssuer_NewIssuerURLKeepsRestatedEndpoints(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, discoveredDraftPayload("update-moved-restated", upstream))
	require.NoError(t, err)
	id := uuid.MustParse(created.ID)

	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &issuersgen.UpdateRemoteSessionIssuerPayload{
		ID:                                created.ID,
		Issuer:                            conv.PtrEmpty("https://moved.example.com"),
		AuthorizationEndpoint:             conv.PtrEmpty("https://moved.example.com/authorize"),
		TokenEndpoint:                     conv.PtrEmpty("https://moved.example.com/token"),
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
	})
	require.NoError(t, err)
	moved := loadIssuerByID(t, ctx, ti, id)
	require.Equal(t, "https://moved.example.com/authorize", moved.AuthorizationEndpoint.String)
	require.Equal(t, "https://moved.example.com/token", moved.TokenEndpoint.String)
	require.Equal(t, []string{"client_secret_post"}, moved.TokenEndpointAuthMethodsSupported)
	require.False(t, moved.RegistrationEndpoint.Valid, "an endpoint the edit did not restate belongs to the old issuer")
	require.False(t, moved.JwksUri.Valid)
	require.Empty(t, moved.ScopesSupported)
}

func TestUpdateIssuer_NewIssuerURLDiscardsDiscoveryOnOrganizationAndGlobalTiers(t *testing.T) {
	t.Parallel()
	upstream := newCreateDiscoveryUpstream(t)
	ctx, ti := newTestService(t)

	org, err := ti.service.CreateIssuer(ctx, discoveredOrgDraftPayload("org-update-moved", nil, upstream))
	require.NoError(t, err)
	_, err = ti.service.UpdateIssuer(ctx, &orgissuersgen.UpdateIssuerPayload{ID: org.ID, Issuer: conv.PtrEmpty("https://moved.example.com")})
	require.NoError(t, err)
	orgRow := loadIssuerByID(t, ctx, ti, uuid.MustParse(org.ID))
	require.False(t, orgRow.MetadataFetchedAt.Valid)
	require.Empty(t, orgRow.AuthorizationGrantProfilesSupported)
	require.False(t, orgRow.TokenEndpoint.Valid)
	require.Empty(t, orgRow.GrantTypesSupported)

	adminCtx := withAdmin(t, ctx)
	payload := createGlobalIssuer(t, "global-update-moved")
	payload.Issuer = upstream.server.URL
	payload.AuthorizationEndpoint = conv.PtrEmpty(upstream.server.URL + "/authorize")
	payload.TokenEndpoint = conv.PtrEmpty(upstream.server.URL + "/token")
	payload.GrantTypesSupported = servedGrantTypes
	payload.AuthorizationGrantProfilesSupported = []string{}
	global, err := ti.service.CreateGlobalIssuer(adminCtx, payload)
	require.NoError(t, err)
	require.True(t, loadIssuerByID(t, ctx, ti, uuid.MustParse(global.ID)).MetadataFetchedAt.Valid)
	_, err = ti.service.UpdateGlobalIssuer(adminCtx, &adminrsgen.UpdateGlobalIssuerPayload{ID: global.ID, Issuer: conv.PtrEmpty("https://moved.example.com")})
	require.NoError(t, err)
	globalRow := loadIssuerByID(t, ctx, ti, uuid.MustParse(global.ID))
	require.False(t, globalRow.MetadataFetchedAt.Valid)
	require.Empty(t, globalRow.AuthorizationGrantProfilesSupported)
	require.False(t, globalRow.TokenEndpoint.Valid)
	require.Empty(t, globalRow.GrantTypesSupported)
}
