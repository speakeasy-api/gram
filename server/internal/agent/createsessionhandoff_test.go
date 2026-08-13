package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// serveHandoff drives the public serving handler the way the router would,
// with the token bound as a chi URL param.
func serveHandoff(t *testing.T, ti *testInstance, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/shared/handoffs/"+token, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeSessionHandoff(w, r))
	return w
}

// The full capability lifecycle: mint → audit → serve exactly once → dead.
func TestCreateSessionHandoff_MintsAndServesOnce(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionHandoffExport)
	require.NoError(t, err)

	const doc = "# Session handoff — quokka cutover\n\nOriginal task…\n"
	res, err := ti.service.CreateSessionHandoff(userCtx, &gen.CreateSessionHandoffPayload{
		SessionID:     uuid.NewString(),
		Content:       doc,
		SourceSurface: conv.PtrEmpty("claude-code"),
		TTLSeconds:    conv.PtrEmpty(300),
		SerialNumber:  conv.PtrEmpty("C02XK1ABCDEF"),
		Hostname:      conv.PtrEmpty("dev-macbook-pro"),
	})
	require.NoError(t, err)

	require.Contains(t, res.URL, "/shared/handoffs/")
	token := res.URL[strings.LastIndex(res.URL, "/")+1:]
	require.Len(t, token, 64, "token must be 32 random bytes hex-encoded")
	expires, err := time.Parse(time.RFC3339, res.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(5*time.Minute), expires, time.Minute)

	// The governance record is content-free: size and bounds, never the doc.
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatSessionHandoffExport)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionChatSessionHandoffExport)
	require.NoError(t, err)
	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.InDelta(t, len(doc), meta["content_bytes"], 0)
	require.InDelta(t, 300, meta["ttl_seconds"], 0)
	require.Equal(t, "claude-code", meta["source_surface"])
	require.Equal(t, "c02xk1abcdef", meta["device_serial"])
	require.NotContains(t, string(rec.Metadata), "quokka", "audit metadata must never carry handoff content")
	require.NotContains(t, string(rec.Metadata), token, "audit metadata must never carry the capability token")

	// First read serves the document with single-use cache posture.
	first := serveHandoff(t, ti, token)
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, doc, first.Body.String())
	require.Equal(t, "no-store", first.Result().Header.Get("Cache-Control"))
	require.Contains(t, first.Result().Header.Get("Content-Type"), "text/markdown")

	// Burn-after-read: the same token is dead on the second fetch.
	second := serveHandoff(t, ti, token)
	require.Equal(t, http.StatusNotFound, second.Code)
}

// The fleet-shared org install key must not be able to upload content and
// mint public-by-token URLs in an employee's name (DNO-383 posture).
func TestCreateSessionHandoff_InstallKeyRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	_, err := ti.service.CreateSessionHandoff(ctx, &gen.CreateSessionHandoffPayload{
		SessionID:     uuid.NewString(),
		Content:       "# doc",
		SourceSurface: nil,
		TTLSeconds:    nil,
		SerialNumber:  nil,
		Hostname:      nil,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "per-user agent key")
}

// The org feature gate keeps the surface dark until explicit enrollment.
func TestCreateSessionHandoff_FeatureDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	ti.features.sessionPortability = false
	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")

	_, err := ti.service.CreateSessionHandoff(userCtx, &gen.CreateSessionHandoffPayload{
		SessionID:     uuid.NewString(),
		Content:       "# doc",
		SourceSurface: nil,
		TTLSeconds:    nil,
		SerialNumber:  nil,
		Hostname:      nil,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "not enabled")
}

// Requested lifetimes clamp into [1m, 1h] so a caller can neither mint an
// instantly-dead link by accident nor an effectively permanent one on purpose.
func TestCreateSessionHandoff_TTLClamped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	userCtx := withPerUserKeyAuth(t, ctx, "dev@acme.corp")

	low, err := ti.service.CreateSessionHandoff(userCtx, &gen.CreateSessionHandoffPayload{
		SessionID:     uuid.NewString(),
		Content:       "# doc",
		SourceSurface: nil,
		TTLSeconds:    conv.PtrEmpty(1),
		SerialNumber:  nil,
		Hostname:      nil,
	})
	require.NoError(t, err)
	lowExpires, err := time.Parse(time.RFC3339, low.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Minute), lowExpires, 30*time.Second)

	high, err := ti.service.CreateSessionHandoff(userCtx, &gen.CreateSessionHandoffPayload{
		SessionID:     uuid.NewString(),
		Content:       "# doc",
		SourceSurface: nil,
		TTLSeconds:    conv.PtrEmpty(999999),
		SerialNumber:  nil,
		Hostname:      nil,
	})
	require.NoError(t, err)
	highExpires, err := time.Parse(time.RFC3339, high.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), highExpires, time.Minute)
}

// An expired row is a 404 indistinguishable from a bogus token, and malformed
// tokens never reach the database.
func TestServeSessionHandoff_ExpiredAndMalformed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	expiredToken := strings.Repeat("ab", 32)
	_, err := repo.New(ti.conn).InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		SessionID:      uuid.NewString(),
		Token:          expiredToken,
		Content:        "# stale",
		CreatedByEmail: "dev@acme.corp",
		ExpiresAt:      conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
	})
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, serveHandoff(t, ti, expiredToken).Code)
	require.Equal(t, http.StatusNotFound, serveHandoff(t, ti, "short").Code)
	require.Equal(t, http.StatusNotFound, serveHandoff(t, ti, strings.Repeat("z", 200)).Code)
}
