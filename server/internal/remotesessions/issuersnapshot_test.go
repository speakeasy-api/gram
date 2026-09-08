package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// storedSnapshot returns the decoded remote_session_issuers.metadata for an
// issuer, or nil when the row has never captured one.
func storedSnapshot(t *testing.T, ctx context.Context, ti *testInstance, created *types.RemoteSessionIssuer) map[string]any {
	t.Helper()

	stored, err := repo.New(ti.conn).GetRemoteSessionIssuerByIDProjectOwned(ctx, repo.GetRemoteSessionIssuerByIDProjectOwnedParams{
		ID:        uuid.MustParse(created.ID),
		ProjectID: uuid.NullUUID{UUID: uuid.MustParse(created.ProjectID), Valid: true},
	})
	require.NoError(t, err)

	if len(stored.Metadata) == 0 {
		return nil
	}

	decoded := map[string]any{}
	require.NoError(t, json.Unmarshal(stored.Metadata, &decoded))
	return decoded
}

// A refresh writes the snapshot and the typed columns from one document in one
// statement, so the two can never disagree about which authorization server the
// issuer is.
func TestRefreshRemoteSessionIssuerMetadata_CapturesSnapshot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		// Fields the typed columns do not model. They are the whole reason the
		// snapshot exists, so they must survive into it.
		doc["userinfo_endpoint"] = "https://idp.example.com/userinfo"
		doc["claims_supported"] = []string{"sub", "email"}
	})

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-snapshot-refresh", upstream.URL))
	require.NoError(t, err)
	require.Nil(t, storedSnapshot(t, ctx, ti, created), "create stores no snapshot; refresh is the capture event")

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	snapshot := storedSnapshot(t, ctx, ti, created)
	require.NotNil(t, snapshot, "refresh is the capture event")
	require.Equal(t, upstream.URL, snapshot["issuer"])
	require.Equal(t, upstream.URL+"/authorize", snapshot["authorization_endpoint"])
	require.Equal(t, "https://idp.example.com/userinfo", snapshot["userinfo_endpoint"])
	require.Equal(t, []any{"sub", "email"}, snapshot["claims_supported"])
}

// The snapshot is re-served from Gram's origin, so it must not carry values the
// typed columns drop. Otherwise a client's view of the issuer would depend on
// whether the row had been refreshed yet.
func TestRefreshRemoteSessionIssuerMetadata_SnapshotDropsUnsafeURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		doc["revocation_endpoint"] = "http://idp.example.com/revoke"
		doc["service_documentation"] = "javascript:alert(1)"
	})

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-snapshot-unsafe", upstream.URL))
	require.NoError(t, err)

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	snapshot := storedSnapshot(t, ctx, ti, created)
	require.NotNil(t, snapshot)
	require.NotContains(t, snapshot, "revocation_endpoint", "a plaintext revocation endpoint is dropped from the typed column too")
	require.NotContains(t, snapshot, "service_documentation")
}

// Create persists an operator-submitted form and deliberately does no
// discovery of its own: the snapshot is only needed by direct upstream
// authorization, and that project is responsible for ensuring an issuer has one
// before allowing an MCP server to depend on it. Optimistically fetching a
// document on every issuer create is not worth the round trip.
//
// The counter is the assertion. A create that quietly probes the upstream is
// exactly the regression this guards, and it would otherwise be invisible.
func TestCreateRemoteSessionIssuer_DoesNotDiscover(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var probes atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		http.NotFound(w, nil)
	}))
	t.Cleanup(upstream.Close)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-snapshot-nocapture", upstream.URL))
	require.NoError(t, err)

	require.Zero(t, probes.Load(), "create must not reach out to the issuer")
	require.Nil(t, storedSnapshot(t, ctx, ti, created), "no document was fetched, so there is nothing to snapshot")
}

// An issuer whose upstream is unreachable is created exactly as before: there
// was never a fetch to fail.
func TestCreateRemoteSessionIssuer_UnreachableUpstreamStillCreates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-snapshot-unreachable"))
	require.NoError(t, err)
	require.Nil(t, storedSnapshot(t, ctx, ti, created))
}
