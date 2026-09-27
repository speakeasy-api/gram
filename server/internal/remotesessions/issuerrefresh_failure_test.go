package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Only failure tracking and updated_at may change after failed discovery.
func requireIssuerSnapshotUnchanged(t *testing.T, before, after repo.RemoteSessionIssuer) {
	t.Helper()
	after.MetadataLastError = before.MetadataLastError
	after.MetadataLastErrorAt = before.MetadataLastErrorAt
	after.MetadataLastErrorUrl = before.MetadataLastErrorUrl
	after.UpdatedAt = before.UpdatedAt
	require.Equal(t, before, after)
}

func TestIssuerMetadataRefresh_PartialReadPreservesOIDCSnapshot(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	refresher, _ := newIssuerMetadataRefresher(t, ti)
	var oidcStatus atomic.Int32
	oidcStatus.Store(http.StatusOK)
	upstream := metadataServer(t, metadataServerOptions{oidcStatus: &oidcStatus})
	id := createProjectIssuer(t, ctx, ti, "partial-oidc", upstream.URL)
	candidate := refreshCandidate(t, ctx, ti, id)
	outcome, err := refresher.Refresh(ctx, candidate)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeRefreshed, outcome)
	before := loadIssuerByID(t, ctx, ti, id)
	require.True(t, before.UserinfoEndpoint.Valid)
	require.NotEmpty(t, before.Jwks)

	oidcStatus.Store(http.StatusServiceUnavailable)
	outcome, err = refresher.Refresh(ctx, candidate)
	require.NoError(t, err)
	require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure, outcome)
	after := loadIssuerByID(t, ctx, ti, id)
	requireIssuerSnapshotUnchanged(t, before, after)
	require.Contains(t, after.MetadataLastError.String, "503")
	require.Equal(t, upstream.URL+"/.well-known/openid-configuration", after.MetadataLastErrorUrl.String)
}

func TestIssuerMetadataRefresh_CanceledDiscoveryRecordsFailure(t *testing.T) {
	t.Parallel()
	for _, manual := range []bool{false, true} {
		name := "on-use"
		if manual {
			name = "manual"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			refreshCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				cancel()
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(upstream.Close)
			created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("canceled-discovery", upstream.URL))
			require.NoError(t, err)
			id := uuid.MustParse(created.ID)
			before := loadIssuerByID(t, ctx, ti, id)
			if manual {
				_, err = ti.service.RefreshRemoteSessionIssuerMetadata(refreshCtx, &gen.RefreshRemoteSessionIssuerMetadataPayload{ID: created.ID})
				requireOopsCode(t, err, oops.CodeGatewayError)
			} else {
				refresher, _ := newIssuerMetadataRefresher(t, ti)
				outcome, err := refresher.Refresh(refreshCtx, refreshCandidate(t, ctx, ti, id))
				require.NoError(t, err)
				require.Equal(t, remotesessionmetrics.IssuerMetadataRefreshOutcomeTransientFailure, outcome)
			}
			require.ErrorIs(t, refreshCtx.Err(), context.Canceled)
			after := loadIssuerByID(t, ctx, ti, id)
			requireIssuerSnapshotUnchanged(t, before, after)
			require.True(t, after.MetadataLastErrorAt.Valid)
			require.True(t, after.MetadataLastErrorUrl.Valid)
		})
	}
}
