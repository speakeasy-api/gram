package oktaresourceconnections_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// Readiness reports the scopes a login would request from the cached
// resource row and never probes the resource itself.
func TestList_ReadsCachedResourceScopesWithoutProbing(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	si.flags.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, si.orgID, true)
	recordAgent(t, ctx, si, "wlp1")

	hits := new(atomic.Int32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == wellknown.OAuthProtectedResourcePath {
			hits.Add(1)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	projectID := createProject(t, ctx, si, si.orgID, "proj-"+uuid.NewString()[:8])
	issuerID := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	// A client with no scope of its own leaves the decision to the resource.
	_, err := si.q.CreateIssuerClientFixture(ctx, repo.CreateIssuerClientFixtureParams{
		ProjectID:             uuid.NullUUID{UUID: projectID, Valid: true},
		OrganizationID:        pgtype.Text{},
		RemoteSessionIssuerID: issuerID,
		ClientID:              "0oascopeless",
		Scope:                 []string{},
		ResourceIdentifier:    pgtype.Text{},
	})
	require.NoError(t, err)
	backendID, err := si.q.CreateRemoteBackendFixture(ctx, repo.CreateRemoteBackendFixtureParams{ProjectID: projectID, Name: conv.ToPGText("Probed"), Slug: conv.ToPGText("probed-" + uuid.NewString()[:8]), Url: upstream.URL})
	require.NoError(t, err)
	serverID, err := si.q.CreateEligibleMCPServerFixture(ctx, repo.CreateEligibleMCPServerFixtureParams{
		ProjectID:             projectID,
		Name:                  conv.ToPGText("Probed"),
		Slug:                  conv.ToPGText("probed-" + uuid.NewString()[:8]),
		RemoteMcpServerID:     uuid.NullUUID{UUID: backendID, Valid: true},
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		UserSessionIssuerID:   uuid.NullUUID{},
		Visibility:            "private",
	})
	require.NoError(t, err)

	// No row yet: the issuer's catalogue stands, and nothing is probed.
	require.Equal(t, []string{"okta.users.read"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)
	require.EqualValues(t, 0, hits.Load())

	rq := remotemcprepo.New(si.conn)
	_, err = rq.UpsertRemoteProtectedResource(ctx, remotemcprepo.UpsertRemoteProtectedResourceParams{
		ProjectID:              projectID,
		OrganizationID:         si.orgID,
		ResourceIdentifier:     upstream.URL,
		MetadataUrl:            "",
		AuthorizationServers:   []string{audience},
		ScopesSupported:        []string{"files:read", "files:write"},
		BearerMethodsSupported: nil,
		ResourceName:           "",
		ResourceDocumentation:  "",
		ResourcePolicyUri:      "",
		ResourceTosUri:         "",
		Metadata:               "",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"files:read", "files:write"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)

	n, err := rq.SetRemoteProtectedResourceScopeOverride(ctx, remotemcprepo.SetRemoteProtectedResourceScopeOverrideParams{ScopeOverride: []string{"legacy:all"}, ProjectID: projectID, ResourceIdentifier: upstream.URL})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Equal(t, []string{"legacy:all"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)

	// Not enrolled: the row is ignored again.
	si.flags.SetFlag(feature.FlagRemoteSessionLiveResourceScopes, si.orgID, false)
	require.Equal(t, []string{"okta.users.read"}, rowFor(t, list(t, ctx, si, true), serverID).Scopes)
	require.EqualValues(t, 0, hits.Load(), "readiness never probes the resource")
}
