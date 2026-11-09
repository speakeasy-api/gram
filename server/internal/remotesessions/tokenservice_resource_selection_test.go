package remotesessions_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestClientNeedsReconnect_CompatibilitySiblingGrant(t *testing.T) {
	t.Parallel()
	for _, expanded := range []bool{false, true} {
		name := "legacy-index"
		if expanded {
			name = "expanded-index"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, f := newAuthClientTokenFixture(t, "sibling-readiness")
			if expanded {
				expandResourceGrantIndexes(t, ctx, f.ti.conn)
			}
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			subject := urn.NewUserSubject(auth.UserID)
			encrypted, err := f.enc.Encrypt([]byte("access-b"))
			require.NoError(t, err)
			_, err = repo.New(f.ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
				SubjectUrn: subject, UserSessionIssuerID: f.signInA, RemoteSessionClientID: f.clientID,
				AccessTokenEncrypted: encrypted, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
				Scopes: []string{}, Resource: conv.ToPGText("https://resource-b.example/mcp"),
			})
			require.NoError(t, err)
			reconnect, err := f.mgr.ClientNeedsReconnect(ctx, f.projectID, f.organizationID, f.signInA, subject, f.clientID, "https://resource-a.example/mcp")
			require.NoError(t, err)
			require.True(t, reconnect, "a singleton for B must prompt connecting A in compatibility mode")
			req := f.request(subject, f.clientID)
			req.Resource = "https://resource-a.example/mcp"
			reconnect, err = f.mgr.AuthClientNeedsReconnect(ctx, req)
			require.NoError(t, err)
			require.True(t, reconnect)
			tokens, err := f.mgr.ResolveAuthClientAccessTokens(ctx, req)
			require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
			require.Empty(t, tokens, "B's bound token must not serve A")
			reconnect, err = f.mgr.ClientNeedsReconnect(ctx, f.projectID, f.organizationID, f.signInA, subject, f.clientID, "https://resource-b.example/mcp/")
			require.NoError(t, err)
			require.False(t, reconnect)
		})
	}
}

func TestResourceTokenSelection_LegacyMultiUpstreamRefreshStaysUnbound(t *testing.T) {
	t.Parallel()
	for _, typed := range []bool{false, true} {
		name := "sign-in"
		if typed {
			name = "auth-client"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var refreshCount atomic.Int64
			var captured atomic.Value
			ctx, fx := seedRefreshableSharedGrant(t, "legacy-selection", newResourceCapturingRefreshHandler(&refreshCount, &captured, "rotated-unbound"))
			const resourceA = "https://resource-a.example/mcp"
			const resourceB = "https://resource-b.example/mcp"
			attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerA, "selection-a", resourceA)
			attachRemoteMcpServerToIssuer(t, ctx, fx.ti.conn, fx.projectID, fx.issuerB, "selection-b", resourceB)
			require.NoError(t, testrepo.New(fx.ti.conn).ExpireRemoteSessionAccessTokenFixture(ctx, fx.session.ID))
			for _, resource := range []string{resourceA, resourceB} {
				if typed {
					tokens, err := fx.mgr.ResolveAuthClientAccessTokens(ctx, remotesessions.AuthClientTokenRequest{
						ProjectID:           fx.projectID,
						OrganizationID:      fx.organizationID,
						UserSessionIssuerID: fx.issuerB,
						Subject:             fx.subject,
						RegistrationID:      fx.clientID,
						Resource:            resource,
					})
					require.NoError(t, err)
					require.Len(t, tokens, 1)
					for _, token := range tokens {
						require.Equal(t, "rotated-unbound", token.Token)
						require.Empty(t, token.Resource)
					}
				} else {
					tokens, err := fx.mgr.ResolveClientAccessTokens(ctx, remotesessions.RefreshAttribution{}, fx.projectID, fx.organizationID, fx.issuerB, fx.subject, fx.clientID, false, resource)
					require.NoError(t, err)
					require.Len(t, tokens, 1)
					for _, token := range tokens {
						require.Equal(t, "rotated-unbound", token.Token)
						require.Empty(t, token.Resource)
					}
				}
			}
			require.Equal(t, int64(1), refreshCount.Load())
			require.Equal(t, tokenPostCapture{HasResource: false, Resource: ""}, captured.Load())
			stored, err := repo.New(fx.ti.conn).GetRemoteSessionByID(ctx, repo.GetRemoteSessionByIDParams{ID: fx.session.ID, ProjectID: fx.projectID, OrganizationID: fx.organizationID})
			require.NoError(t, err)
			require.False(t, stored.Resource.Valid, "selection must not backfill a legacy multi-upstream grant")
		})
	}
}
