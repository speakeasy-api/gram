package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// TestHandleIDPCallback_RealResolverMembershipReconciliation exercises the real
// identity resolver, PostgreSQL, and Redis. Only external WorkOS HTTP endpoints
// are mocked, so stale-cache assertions cover the full login bootstrap.
func TestHandleIDPCallback_RealResolverMembershipReconciliation(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		localMember    bool
		primeCache     bool
		remoteMember   bool
		listingFailure bool
		wantMember     bool
		wantError      bool
	}{
		{name: "missing_local_membership", remoteMember: true, wantMember: true},
		{name: "stale_cached_negative", primeCache: true, remoteMember: true, wantMember: true},
		{name: "revoked_stale_positive", localMember: true, primeCache: true, wantError: true},
		{name: "listing_failure_stale_positive", localMember: true, primeCache: true, listingFailure: true, wantMember: true, wantError: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// The service retains this pointer; initialize it once its database and cache exist.
			resolver := new(identity.Resolver)
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, resolver)
			toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
			ac, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			uid := "test-" + uuid.NewString()
			wid := "user_" + uuid.NewString()
			oid := "org_" + uuid.NewString()
			mid := "om_" + uuid.NewString()
			email := uid + "@example.com"
			_, err := ti.conn.Exec(ctx, "INSERT INTO users (id,email,display_name,workos_id) VALUES ($1,$2,'Test User',$3)", uid, email, wid)
			require.NoError(t, err)
			_, err = ti.conn.Exec(ctx, "UPDATE organization_metadata SET workos_id=$1 WHERE id=$2", oid, ac.ActiveOrganizationID)
			require.NoError(t, err)
			if scenario.localMember {
				_, err = ti.conn.Exec(ctx, "INSERT INTO organization_user_relationships (organization_id,user_id,workos_user_id,workos_membership_id) VALUES ($1,$2,$3,$4)", ac.ActiveOrganizationID, uid, wid, mid)
				require.NoError(t, err)
			}
			var exchangeCalls, listingCalls atomic.Int32
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				user := map[string]any{
					"id":             wid,
					"email":          email,
					"first_name":     "Test",
					"last_name":      "User",
					"external_id":    uid,
					"email_verified": true,
				}
				switch {
				case r.Method == "POST" && r.URL.Path == "/user_management/authenticate":
					exchangeCalls.Add(1)
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode exchange request: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if body["code"] != "test-auth-code" {
						t.Error("incorrect exchange code")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "test-token", "user": user})
				case r.Method == "GET" && r.URL.Path == "/user_management/users/"+wid:
					_ = json.NewEncoder(w).Encode(user)
				case r.Method == "GET" && r.URL.Path == "/user_management/organization_memberships":
					listingCalls.Add(1)
					if r.URL.Query().Get("user_id") != wid || r.URL.Query().Get("statuses") != "active" {
						t.Error("incorrect membership filters")
					}
					if scenario.listingFailure {
						w.WriteHeader(http.StatusBadGateway)
						_, _ = w.Write([]byte(`{"message":"test listing unavailable"}`))
						return
					}
					data := []any{}
					if scenario.remoteMember {
						data = append(data, map[string]any{
							"id":                mid,
							"user_id":           wid,
							"organization_id":   oid,
							"organization_name": "Test Org",
							"status":            "active",
							"role":              map[string]string{"slug": "member"},
						})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "list_metadata": map[string]any{}})
				default:
					t.Errorf("unexpected remote request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			t.Cleanup(remote.Close)
			sdk := usermanagement.NewClient("test-key")
			sdk.Endpoint = remote.URL
			sdk.HTTPClient = remote.Client()
			policy, err := guardian.NewUnsafePolicy(ti.tracerProvider, []string{})
			require.NoError(t, err)
			p, err := pylon.NewPylon(ti.logger, "")
			require.NoError(t, err)
			*resolver = *identity.NewResolver(
				ti.logger, ti.tracerProvider, ti.cacheAdapter, remote.URL, "test-client",
				identity.NewWorkOSAdapter(sdk),
				workos.NewClient(policy, "test-key", workos.ClientOpts{Endpoint: remote.URL}),
				orgrepo.New(ti.conn), userrepo.New(ti.conn), p, nil, nil,
				testenv.NewCacheSuffix(t, cache.Suffix("idp-callback")),
			)
			memberBefore, err := resolver.IsOrganizationMember(ctx, ac.ActiveOrganizationID, uid)
			require.NoError(t, err)
			require.Equal(t, scenario.localMember, memberBefore)
			if scenario.primeCache {
				_, _, access := resolver.HasAccessToOrganization(ctx, ac.ActiveOrganizationID, uid)
				require.Equal(t, scenario.localMember, access)
				_, hit, err := resolver.GetUserInfo(ctx, uid)
				require.NoError(t, err)
				require.True(t, hit)
			}
			state := uuid.NewString()
			require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
				ID:                  state,
				UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
				Endpoint: mcp.EndpointRef{
					McpSlug:        toolset.McpSlug.String,
					CustomDomainID: toolset.CustomDomainID,
				},
				ClientID:            "test-client",
				RedirectURI:         "http://localhost:3000/callback",
				State:               "client-state",
				CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
				CodeChallengeMethod: "S256",
				CSRFToken:           "csrf-token",
				CreatedAt:           time.Now(),
			}))
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/mcp/idp_callback?state="+state+"&code=test-auth-code", nil).WithContext(ctx)
			err = ti.service.HandleIDPCallback(w, req)
			require.EqualValues(t, 1, exchangeCalls.Load())
			require.GreaterOrEqual(t, listingCalls.Load(), int32(1))
			if scenario.wantError {
				require.Error(t, err)
				require.Empty(t, w.Header().Get("Location"))
				var shared *oops.ShareableError
				require.ErrorAs(t, err, &shared)
				if !scenario.listingFailure {
					require.Equal(t, oops.CodeForbidden, shared.Code)
				} else {
					require.Equal(t, oops.CodeUnexpected, shared.Code)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, http.StatusFound, w.Code)
				redirect, parseErr := url.Parse(w.Header().Get("Location"))
				require.NoError(t, parseErr)
				require.Equal(t, "/mcp/"+toolset.McpSlug.String+"/connect", redirect.Path)
				rotatedID := redirect.Query().Get("state")
				require.NotEmpty(t, rotatedID)
				require.NotEqual(t, state, rotatedID)
				rotated, cacheErr := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+rotatedID)
				require.NoError(t, cacheErr)
				require.Equal(t, uid, rotated.AuthorizerUserID)
				require.NotNil(t, rotated.Subject)
				require.Equal(t, urn.NewUserSubject(uid), *rotated.Subject)
			}
			member, err := resolver.IsOrganizationMember(ctx, ac.ActiveOrganizationID, uid)
			require.NoError(t, err)
			require.Equal(t, scenario.wantMember, member)
			if !scenario.listingFailure {
				_, _, access := resolver.HasAccessToOrganization(ctx, ac.ActiveOrganizationID, uid)
				require.Equal(t, scenario.wantMember, access)
			}
		})
	}
}
