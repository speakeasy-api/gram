package remotesessions_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

type heldRefreshCache struct{ cache.Cache }

func (heldRefreshCache) Add(context.Context, string, time.Duration) (bool, error) { return false, nil }

func TestRefreshNow_ReconnectNeverAdoptsReplacement(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"before-lock", "waiting", "post-invalid-grant", "post-cas-loss"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			arrived, release := make(chan struct{}), make(chan struct{}, 1)
			// Always release a blocked HTTP handler, even on an assertion failure.
			defer close(release)
			var attempts atomic.Int32
			ctx, env := newSyntheticExpiryEnv(t, "reconnect-"+mode, func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				w.Header().Set("Content-Type", "application/json")
				if r.Form.Get("grant_type") != "refresh_token" {
					_, _ = w.Write([]byte(`{"access_token":"original-access","refresh_token":"original-refresh","expires_in":3600}`))
					return
				}
				attempts.Add(1)
				if mode == "post-invalid-grant" || mode == "post-cas-loss" {
					close(arrived)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				if mode == "post-invalid-grant" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				} else {
					_, _ = w.Write([]byte(`{"access_token":"refreshed-original","refresh_token":"rotated-original","expires_in":3600}`))
				}
			})
			reconnect := func() repo.RemoteSession {
				_, err := testrepo.New(env.db).SoftDeleteAttachmentSourceFixture(ctx, env.session.ID)
				require.NoError(t, err)
				replacement, err := env.q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
					SubjectUrn: env.subject, UserSessionIssuerID: env.session.UserSessionIssuerID,
					RemoteSessionClientID: env.clientID, AccessTokenEncrypted: env.session.AccessTokenEncrypted,
					AccessExpiresAt:       conv.ToPGTimestamptz(time.Now().Add(time.Hour)),
					RefreshTokenEncrypted: env.session.RefreshTokenEncrypted, Scopes: []string{},
				})
				require.NoError(t, err)
				require.NotEqual(t, env.session.ID, replacement.ID)
				return replacement
			}
			if mode == "waiting" {
				env.refresher = env.newRefresher(testenv.NewMeterProvider(t), heldRefreshCache{Cache: cache.NoopCache})
			}
			var result remotesessions.RefreshResult
			var refreshErr error
			var replacement repo.RemoteSession
			if mode == "post-invalid-grant" || mode == "post-cas-loss" {
				done := make(chan struct{})
				go func() {
					defer close(done)
					result, refreshErr = env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
				}()
				select {
				case <-arrived:
				case <-time.After(10 * time.Second):
					t.Fatal("refresh did not reach upstream")
				}
				replacement = reconnect()
				// Send rather than close so the deferred close remains safe.
				release <- struct{}{}
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("refresh did not finish")
				}
				require.EqualValues(t, 1, attempts.Load())
			} else {
				replacement = reconnect()
				result, refreshErr = env.refresher.RefreshNow(ctx, env.session, "", remotesessionmetrics.RefreshTriggerScheduled)
				require.Zero(t, attempts.Load(), "replacement credentials must never reach the token endpoint")
			}
			require.NoError(t, refreshErr)
			require.Empty(t, result.AccessToken)
			require.Equal(t, remotesessionmetrics.RefreshOutcomeSessionInactive, result.Outcome)
			current, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)
			require.Equal(t, replacement.ID, current.ID)
			require.Equal(t, replacement.AccessTokenEncrypted, current.AccessTokenEncrypted)
			require.Equal(t, replacement.RefreshTokenEncrypted, current.RefreshTokenEncrypted)
			require.Equal(t, replacement.UpdatedAt, current.UpdatedAt)
		})
	}
}
