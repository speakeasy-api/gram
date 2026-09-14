// The refresh grant omits resource only when an operator says the issuer rejects
// it, and drops it for one retry when the upstream answers invalid_target.

package remotesessions_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestResolveAccessToken_RefreshResourceFollowsIssuerResourceIndicatorSupport(t *testing.T) {
	t.Parallel()

	const resource = "https://mcp.example.com/mcp"
	cases := []struct {
		name         string
		supported    pgtype.Bool
		wantResource bool
	}{
		{name: "unset issuer still sends resource", supported: pgtype.Bool{Bool: false, Valid: false}, wantResource: true},
		{name: "operator true sends resource", supported: pgtype.Bool{Bool: true, Valid: true}, wantResource: true},
		{name: "operator false omits resource", supported: pgtype.Bool{Bool: false, Valid: true}, wantResource: false},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var spy upstreamSpy
			ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "resource-indicator-"+string(rune('a'+i)), pgtype.Text{String: "", Valid: false}, spyRefreshHandler(&spy))

			setIssuerResourceIndicatorSupported(t, ctx, ti, clientID, tc.supported)

			tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, resource)
			require.NoError(t, err)
			require.NoError(t, spy.handlerErr)
			require.Equal(t, "refreshed-access", tok)

			if tc.wantResource {
				require.Equal(t, resource, spy.form.Get("resource"))
			} else {
				require.False(t, spy.form.Has("resource"), "the refresh grant must not send resource to an issuer that rejects it")
			}
		})
	}
}

// refreshRecorder captures every refresh POST's form and answers each from
// respond, so a test can assert how many grants were sent and with what.
type refreshRecorder struct {
	mu    sync.Mutex
	forms []url.Values
}

func (r *refreshRecorder) handler(respond func(form url.Values) (status int, body string)) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r.mu.Lock()
		r.forms = append(r.forms, form)
		r.mu.Unlock()
		status, body := respond(form)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func (r *refreshRecorder) sent() []url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]url.Values(nil), r.forms...)
}

const refreshedTokenBody = `{"access_token":"refreshed-access","token_type":"Bearer","expires_in":3600,"refresh_token":"refreshed-refresh"}`

// rejectResourceThenRefresh answers invalid_target while resource is present
// and a fresh token set once it is dropped.
func rejectResourceThenRefresh(form url.Values) (int, string) {
	if form.Has("resource") {
		return http.StatusBadRequest, `{"error":"invalid_target","error_description":"resource is not recognised"}`
	}
	return http.StatusOK, refreshedTokenBody
}

func setIssuerResourceIndicatorSupported(t *testing.T, ctx context.Context, ti *testInstance, clientID uuid.UUID, supported pgtype.Bool) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	client, err := q.GetRemoteSessionClientWithIssuerByID(ctx, clientID)
	require.NoError(t, err)
	_, err = q.UpdateRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionIssuerParams{
		ResourceIndicatorSupported: supported,
		ID:                         client.RemoteSessionIssuerID,
		ProjectID:                  conv.ToNullUUID(*authCtx.ProjectID),
	})
	require.NoError(t, err)
}

func TestRefreshGrant_InvalidTargetRetriesOnceWithoutResource(t *testing.T) {
	t.Parallel()

	const resource = "https://mcp.example.com/mcp"
	var rec refreshRecorder
	ctx, mgr, _, clientID, subject := setupRefreshFixtureWithHandler(t, "invalid-target-retry", pgtype.Text{String: "", Valid: false}, rec.handler(rejectResourceThenRefresh))

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, resource)
	require.NoError(t, err)
	require.Equal(t, "refreshed-access", tok)

	sent := rec.sent()
	require.Len(t, sent, 2, "exactly one retry")
	require.Equal(t, resource, sent[0].Get("resource"))
	require.False(t, sent[1].Has("resource"), "the retry drops resource")
	require.Equal(t, "refresh_token", sent[1].Get("grant_type"))
	require.Equal(t, sent[0].Get("refresh_token"), sent[1].Get("refresh_token"), "the same grant is presented again")
}

// Only invalid_target names the resource as the problem; every other answer
// is final on the first POST.
func TestRefreshGrant_NoRetryOnOtherErrors(t *testing.T) {
	t.Parallel()

	const resource = "https://mcp.example.com/mcp"
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "invalid_grant", status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"token revoked"}`},
		{name: "server error", status: http.StatusBadGateway, body: `upstream unavailable`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var rec refreshRecorder
			ctx, mgr, _, clientID, subject := setupRefreshFixtureWithHandler(t, "no-retry-"+tc.name, pgtype.Text{String: "", Valid: false}, rec.handler(func(url.Values) (int, string) {
				return tc.status, tc.body
			}))

			// A failed refresh collapses to "no usable token", never an error.
			tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, resource)
			require.NoError(t, err)
			require.Empty(t, tok)

			sent := rec.sent()
			require.Len(t, sent, 1, "no retry")
			require.Equal(t, resource, sent[0].Get("resource"))
		})
	}
}

// An operator's false never sends resource, so invalid_target is a plain
// failure with nothing to drop.
func TestRefreshGrant_OperatorFalseSendsNoResourceAndNeverRetries(t *testing.T) {
	t.Parallel()

	const resource = "https://mcp.example.com/mcp"
	var rec refreshRecorder
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "operator-false-no-retry", pgtype.Text{String: "", Valid: false}, rec.handler(func(url.Values) (int, string) {
		return http.StatusBadRequest, `{"error":"invalid_target","error_description":"no"}`
	}))
	setIssuerResourceIndicatorSupported(t, ctx, ti, clientID, pgtype.Bool{Bool: false, Valid: true})

	tok, err := mgr.ResolveAccessToken(ctx, clientID, subject, resource)
	require.NoError(t, err)
	require.Empty(t, tok)

	sent := rec.sent()
	require.Len(t, sent, 1)
	require.False(t, sent[0].Has("resource"))
}
