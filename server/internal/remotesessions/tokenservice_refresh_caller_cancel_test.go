package remotesessions_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestResolveAccessToken_CallerCancelDuringRefreshKeepsRotatedToken(t *testing.T) {
	t.Parallel()

	requestCtx, cancelRequest := context.WithCancel(t.Context())
	defer cancelRequest()

	var refreshes atomic.Int32
	ctx, env := newExpiredRefreshEnv(t, "refresh-caller-cancel", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The first refresh consumes the old token, as a rotating provider does.
		if refreshes.Add(1) > 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		cancelRequest()
		_, _ = w.Write([]byte(`{"access_token":"access-rotated","refresh_token":"refresh-rotated","token_type":"Bearer","expires_in":3600}`))
	})

	// The caller's outcome is unobservable: the client has gone away.
	_, _ = env.mgr.ResolveAccessToken(contextWithValuesOf(requestCtx, ctx), env.clientID, env.subject, "")
	require.ErrorIs(t, requestCtx.Err(), context.Canceled)

	active, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.True(t, active.RefreshTokenEncrypted.Valid, "the rotated refresh token must be stored")

	token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, "access-rotated", token)
	require.EqualValues(t, 1, refreshes.Load(), "the next request must use the stored token, not refresh again")
}

// contextWithValuesOf is values's context, canceled when cancellation is.
func contextWithValuesOf(cancellation, values context.Context) context.Context {
	ctx, cancel := context.WithCancel(values)
	context.AfterFunc(cancellation, cancel)
	return ctx
}
