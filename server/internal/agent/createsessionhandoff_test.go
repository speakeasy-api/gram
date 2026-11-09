package agent_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sessionhandoff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
	idempotencyKey := "retry-" + uuid.NewString()
	payload := &gen.CreateSessionHandoffPayload{
		SessionID:      uuid.NewString(),
		Content:        doc,
		SourceSurface:  conv.PtrEmpty("claude-code"),
		TTLSeconds:     conv.PtrEmpty(300),
		SerialNumber:   conv.PtrEmpty("C02XK1ABCDEF"),
		Hostname:       conv.PtrEmpty("dev-macbook-pro"),
		IdempotencyKey: &idempotencyKey,
	}
	res, err := ti.service.CreateSessionHandoff(userCtx, payload)
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

	// Capture the blob pointer while the link is still live: the claim blanks
	// it, so after the read there is nothing left to look the object up by.
	minted, err := testrepo.New(ti.conn).GetSessionHandoffLinkFixture(ctx, token)
	require.NoError(t, err)
	require.NotEmpty(t, minted.BlobUrl, "mint must record where the document went")
	blobURL, err := url.Parse(minted.BlobUrl)
	require.NoError(t, err)
	stored, err := ti.blobs.Exists(ctx, blobURL)
	require.NoError(t, err)
	require.True(t, stored, "mint must write the document to object storage")

	// First read serves the document with single-use cache posture.
	first := serveHandoff(t, ti, token)
	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, doc, first.Body.String())
	require.Equal(t, "no-store", first.Result().Header.Get("Cache-Control"))
	require.Equal(t, "no-referrer", first.Result().Header.Get("Referrer-Policy"))
	require.Equal(t, "noindex, nofollow", first.Result().Header.Get("X-Robots-Tag"))
	require.Equal(t, "nosniff", first.Result().Header.Get("X-Content-Type-Options"))
	require.Contains(t, first.Result().Header.Get("Content-Type"), "text/markdown")
	require.Equal(t, `attachment; filename="session-handoff.md"`, first.Result().Header.Get("Content-Disposition"))
	require.Equal(t, "default-src 'none'; sandbox", first.Result().Header.Get("Content-Security-Policy"))

	// Burn-after-read: the same token is dead on the second fetch.
	second := serveHandoff(t, ti, token)
	require.Equal(t, http.StatusNotFound, second.Code)

	// Burning the link also destroys the server's copy: the row survives for
	// its consumed_at bookkeeping, but the blob pointer does not outlive the
	// read — and the blob itself is deleted from object storage.
	burned, err := testrepo.New(ti.conn).GetSessionHandoffLinkFixture(ctx, token)
	require.NoError(t, err)
	require.Empty(t, burned.BlobUrl, "consumed handoff must not retain the blob pointer")
	require.True(t, burned.ConsumedAt.Valid, "consumed handoff must record when it was burned")
	gone, err := ti.blobs.Exists(ctx, blobURL)
	require.NoError(t, err)
	require.False(t, gone, "burning a handoff must delete the document from object storage")

	// The consumed row remains an idempotency tombstone. A retry returns the
	// original, now-dead capability and expiry instead of minting a replacement.
	replayed, err := ti.service.CreateSessionHandoff(userCtx, payload)
	require.NoError(t, err)
	require.Equal(t, res, replayed)
}

func TestSessionHandoffMinterSeparatesCanonicalChatAndProductSessionIDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	canonicalChatID := uuid.New()
	const externalSessionID = "external-session-not-a-chat-uuid"
	minter := sessionhandoff.NewSessionHandoffMinter(testenv.NewLogger(t), ti.conn, ti.blobs, "https://example.test", audit.NewLogger())
	result, err := minter.Mint(ctx, sessionhandoff.SessionHandoffMintInput{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, UserID: "user_canonical_audit",
		ChatID: canonicalChatID, SessionID: externalSessionID, Content: "# redacted handoff", SourceSurface: "platform-mcp",
	})
	require.NoError(t, err)
	token := result.URL[strings.LastIndex(result.URL, "/")+1:]
	persisted, err := testrepo.New(ti.conn).GetSessionHandoffLinkFixture(ctx, token)
	require.NoError(t, err)
	require.Equal(t, externalSessionID, persisted.SessionID)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionChatSessionHandoffExport)
	require.NoError(t, err)
	require.Equal(t, canonicalChatID.String(), record.SubjectID)
	require.Equal(t, "chat_session", record.SubjectType)
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

	// A lifetime absurd enough to overflow the nanosecond conversion must still
	// land on the floor: clamping after the multiplication would wrap this past
	// the minimum and hand back the maximum instead.
	overflow, err := ti.service.CreateSessionHandoff(userCtx, &gen.CreateSessionHandoffPayload{
		SessionID:     uuid.NewString(),
		Content:       "# doc",
		SourceSurface: nil,
		TTLSeconds:    conv.PtrEmpty(-1 << 40),
		SerialNumber:  nil,
		Hostname:      nil,
	})
	require.NoError(t, err)
	overflowExpires, err := time.Parse(time.RFC3339, overflow.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Minute), overflowExpires, 30*time.Second)
}

// An expired row is a 404 indistinguishable from a bogus token, and malformed
// tokens never reach the database.
func TestServeSessionHandoff_ExpiredAndMalformed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	expiredToken := strings.Repeat("ab", 32)
	uploadIntentID, err := repo.New(ti.conn).CreateSessionHandoffUploadIntent(ctx, repo.CreateSessionHandoffUploadIntentParams{
		ActorUserID: "user_test", ObjectPath: "unused/expired.md", BlobUrl: "file://unused/expired.md", ProjectID: ti.projectID, OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	_, err = repo.New(ti.conn).InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
		ProjectID:      ti.projectID,
		OrganizationID: ti.orgID,
		SessionID:      uuid.NewString(),
		Token:          expiredToken,
		BlobUrl:        "file://unused/expired.md",
		ActorUserID:    conv.ToPGText("user_test"),
		CreatedByEmail: "dev@acme.corp",
		ExpiresAt:      conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
		UploadIntentID: uploadIntentID,
	})
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, serveHandoff(t, ti, expiredToken).Code)
	malformed := serveHandoff(t, ti, "short")
	require.Equal(t, http.StatusNotFound, malformed.Code)
	require.Equal(t, "no-store", malformed.Header().Get("Cache-Control"))
	require.Equal(t, "no-referrer", malformed.Header().Get("Referrer-Policy"))
	require.Equal(t, "noindex, nofollow", malformed.Header().Get("X-Robots-Tag"))
	require.Equal(t, "nosniff", malformed.Header().Get("X-Content-Type-Options"))
	require.Equal(t, http.StatusNotFound, serveHandoff(t, ti, strings.Repeat("z", 200)).Code)
	// Right length, wrong alphabet: rejected on shape, never looked up.
	require.Equal(t, http.StatusNotFound, serveHandoff(t, ti, strings.Repeat("z", 64)).Code)
}

func TestCreateSessionHandoff_IdempotentReplayAndConflict(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	userCtx := withPerUserKeyAuth(t, ctx, " creator@example.test ")
	key := "retry-" + uuid.NewString()
	payload := &gen.CreateSessionHandoffPayload{
		SessionID: uuid.NewString(), Content: "# exact document", TTLSeconds: conv.PtrEmpty(300),
		IdempotencyKey: &key,
	}

	first, err := ti.service.CreateSessionHandoff(userCtx, payload)
	require.NoError(t, err)
	second, err := ti.service.CreateSessionHandoff(userCtx, payload)
	require.NoError(t, err)
	require.Equal(t, first, second)

	stored, err := repo.New(ti.conn).GetSessionHandoffLinkByIdempotencyKey(ctx, repo.GetSessionHandoffLinkByIdempotencyKeyParams{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, ActorUserID: conv.ToPGText(authUserID(t, userCtx)), IdempotencyOperation: conv.ToPGText("device_create"), IdempotencyKey: conv.ToPGText(key),
	})
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(userCtx)
	require.True(t, ok)
	require.Equal(t, authCtx.UserID, conv.FromPGTextOrEmpty[string](stored.ActorUserID))
	require.Equal(t, "creator@example.test", stored.CreatedByEmail)

	conflicting := *payload
	conflicting.Content = "# changed document"
	_, err = ti.service.CreateSessionHandoff(userCtx, &conflicting)
	require.ErrorContains(t, err, "idempotency key was already used")
}

func TestCreateSessionHandoff_ConcurrentDuplicateCreatesOneRow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	userCtx := withPerUserKeyAuth(t, ctx, "")
	key := "retry-" + uuid.NewString()
	payload := &gen.CreateSessionHandoffPayload{
		SessionID: uuid.NewString(), Content: "# concurrent document", IdempotencyKey: &key,
	}

	type outcome struct {
		result *gen.CreateSessionHandoffResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	for range 2 {
		go func() {
			<-start
			result, err := ti.service.CreateSessionHandoff(userCtx, payload)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	one, two := <-outcomes, <-outcomes
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.Equal(t, one.result, two.result)

	stored, err := repo.New(ti.conn).GetSessionHandoffLinkByIdempotencyKey(ctx, repo.GetSessionHandoffLinkByIdempotencyKeyParams{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, ActorUserID: conv.ToPGText(authUserID(t, userCtx)), IdempotencyOperation: conv.ToPGText("device_create"), IdempotencyKey: conv.ToPGText(key),
	})
	require.NoError(t, err)
	require.Empty(t, stored.CreatedByEmail)
}

func authUserID(t *testing.T, ctx context.Context) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return authCtx.UserID
}

func TestCreateSessionHandoff_ExpiredCleanupPreservesIdempotencyTombstone(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	userCtx := withPerUserKeyAuth(t, ctx, "dev@example.test")
	userID := authUserID(t, userCtx)
	key := "retry-" + uuid.NewString()
	sessionID := uuid.NewString()
	content := "# expired handoff"
	ttl := 300 * time.Second
	token := strings.Repeat("cd", 32)
	expiresAt := time.Now().Add(-time.Minute).UTC()
	objectPath := "session-handoffs/" + token + ".md"
	writer, blobURL, err := ti.blobs.Write(ctx, objectPath, "text/markdown", int64(len(content)))
	require.NoError(t, err)
	_, err = io.WriteString(writer, content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	queries := repo.New(ti.conn)
	intentID, err := queries.CreateSessionHandoffUploadIntent(ctx, repo.CreateSessionHandoffUploadIntentParams{
		ActorUserID: userID, ObjectPath: objectPath, BlobUrl: blobURL.String(), ProjectID: ti.projectID, OrganizationID: ti.orgID,
	})
	require.NoError(t, err)
	inserted, err := queries.InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
		SessionID: sessionID, Token: token, BlobUrl: blobURL.String(), ActorUserID: conv.ToPGText(userID),
		CreatedByEmail: "dev@example.test", IdempotencyOperation: conv.ToPGText("device_create"),
		IdempotencyKey: conv.ToPGText(key), IdempotencyInputHash: conv.ToPGText(sessionhandoff.DeviceIdempotencyInputHash(sessionID, content, "", "", "", ttl)),
		ExpiresAt: conv.ToPGTimestamptz(expiresAt), UploadIntentID: intentID, ProjectID: ti.projectID, OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	cleaner := sessionhandoff.NewExpiredUnreadCleaner(sessionhandoff.NewPgxExpiredUnreadRepository(ti.conn), ti.blobs)
	_, err = cleaner.Clean(ctx)
	require.NoError(t, err)

	payload := &gen.CreateSessionHandoffPayload{SessionID: sessionID, Content: content, TTLSeconds: conv.PtrEmpty(300), IdempotencyKey: &key}
	replayed, err := ti.service.CreateSessionHandoff(userCtx, payload)
	require.NoError(t, err)
	require.Equal(t, strings.TrimRight(testServerURL, "/")+sessionhandoff.PublicRoutePrefix+token, replayed.URL)
	require.Equal(t, inserted.ExpiresAt.Time.UTC().Format(time.RFC3339), replayed.ExpiresAt)
}

func TestSessionHandoffIdempotencyOperationScopeAcrossProjects(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	secondProjectID := seedProject(t, ctx, ti.conn, ti.orgID, "handoff-second")
	queries := repo.New(ti.conn)
	actorID := "user_scope_test"
	insert := func(projectID uuid.UUID, operation, key, token string) error {
		t.Helper()
		blobURL := "file://unused/" + token + ".md"
		intentID, err := queries.CreateSessionHandoffUploadIntent(ctx, repo.CreateSessionHandoffUploadIntentParams{
			ActorUserID: actorID, ObjectPath: token + ".md", BlobUrl: blobURL, ProjectID: projectID, OrganizationID: ti.orgID,
		})
		if err != nil {
			return fmt.Errorf("create upload intent: %w", err)
		}
		_, err = queries.InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
			SessionID: uuid.NewString(), Token: token, BlobUrl: blobURL, ActorUserID: conv.ToPGText(actorID), CreatedByEmail: "",
			IdempotencyOperation: conv.ToPGText(operation), IdempotencyKey: conv.ToPGText(key), IdempotencyInputHash: conv.ToPGText(strings.Repeat("a", 64)),
			ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), UploadIntentID: intentID, ProjectID: projectID, OrganizationID: ti.orgID,
		})
		if err != nil {
			return fmt.Errorf("insert handoff: %w", err)
		}
		return nil
	}

	platformKey := "platform-" + uuid.NewString()
	require.NoError(t, insert(ti.projectID, "platform_create", platformKey, strings.Repeat("1", 64)))
	require.ErrorIs(t, insert(secondProjectID, "platform_create", platformKey, strings.Repeat("2", 64)), pgx.ErrNoRows)
	platformRow, err := queries.GetSessionHandoffLinkByIdempotencyKey(ctx, repo.GetSessionHandoffLinkByIdempotencyKeyParams{
		OrganizationID: ti.orgID, ActorUserID: conv.ToPGText(actorID), IdempotencyOperation: conv.ToPGText("platform_create"), IdempotencyKey: conv.ToPGText(platformKey), ProjectID: secondProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("1", 64), platformRow.Token)

	deviceKey := "device-" + uuid.NewString()
	require.NoError(t, insert(ti.projectID, "device_create", deviceKey, strings.Repeat("3", 64)))
	require.NoError(t, insert(secondProjectID, "device_create", deviceKey, strings.Repeat("4", 64)))
}

type intentObservingBlobStore struct {
	assets.BlobStore
	queries      *repo.Queries
	projectID    uuid.UUID
	observed     bool
	mismatch     bool
	writerClosed bool
}

func (s *intentObservingBlobStore) Write(ctx context.Context, path, contentType string, contentLength int64) (io.WriteCloser, *url.URL, error) {
	expected, err := s.Location(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve expected location: %w", err)
	}
	rows, err := s.queries.ListStaleSessionHandoffUploadIntents(ctx, repo.ListStaleSessionHandoffUploadIntentsParams{
		StaleBefore: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), BatchSize: 100,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list durable intents: %w", err)
	}
	for _, row := range rows {
		if row.ProjectID == s.projectID && row.BlobUrl == expected.String() {
			s.observed = true
		}
	}
	w, actual, err := s.BlobStore.Write(ctx, path, contentType, contentLength)
	if err != nil {
		return nil, nil, fmt.Errorf("delegate write: %w", err)
	}
	wrapped := &closeObservingWriter{WriteCloser: w, closed: &s.writerClosed}
	if s.mismatch {
		return wrapped, &url.URL{Scheme: "file", Path: actual.Path + ".mismatch"}, nil
	}
	return wrapped, actual, nil
}

type closeObservingWriter struct {
	io.WriteCloser
	closed *bool
}

func (w *closeObservingWriter) Close() error {
	*w.closed = true
	if err := w.WriteCloser.Close(); err != nil {
		return fmt.Errorf("close delegated writer: %w", err)
	}
	return nil
}

func TestSessionHandoffMinterPersistsLocationBeforeWriteAndClosesMismatch(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []bool{false, true} {
		mismatch := mismatch
		t.Run(map[bool]string{false: "matching", true: "mismatch"}[mismatch], func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAgentService(t)
			store := &intentObservingBlobStore{BlobStore: ti.blobs, queries: repo.New(ti.conn), projectID: ti.projectID, mismatch: mismatch}
			minter := sessionhandoff.NewSessionHandoffMinter(testenv.NewLogger(t), ti.conn, store, "https://example.test", audit.NewLogger())
			_, err := minter.Mint(ctx, sessionhandoff.SessionHandoffMintInput{
				OrganizationID: ti.orgID, ProjectID: ti.projectID, UserID: "user_intent_order",
				ChatID: uuid.New(), SessionID: uuid.NewString(), Content: "# durable intent",
			})
			require.True(t, store.observed, "exact blob URL must be durable before Write")
			if mismatch {
				require.ErrorContains(t, err, "different from the durable upload intent")
				require.True(t, store.writerClosed, "mismatched writer must be closed")
			} else {
				require.NoError(t, err)
				require.True(t, store.writerClosed)
			}
		})
	}
}

func TestExpiredCleanerRespectsActiveConsumerLease(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	userCtx := withPerUserKeyAuth(t, ctx, "dev@example.test")
	result, err := ti.service.CreateSessionHandoff(userCtx, &gen.CreateSessionHandoffPayload{SessionID: uuid.NewString(), Content: "# leased"})
	require.NoError(t, err)
	token := result.URL[strings.LastIndex(result.URL, "/")+1:]
	claimedAfter := time.Now().Add(-time.Second)
	claim, err := repo.New(ti.conn).ConsumeSessionHandoffLink(ctx, token)
	require.NoError(t, err)
	claimedRow, err := testrepo.New(ti.conn).GetSessionHandoffLinkFixture(ctx, token)
	require.NoError(t, err)
	require.True(t, claimedRow.BlobCleanupAttemptedAt.Valid, "consume claim must atomically start its cleanup lease")
	require.True(t, claimedRow.BlobCleanupAttemptedAt.Time.After(claimedAfter))
	blobURL, err := url.Parse(claim.BlobUrl)
	require.NoError(t, err)

	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.ExpireSessionHandoffLinkFixture(ctx, testrepo.ExpireSessionHandoffLinkFixtureParams{
		ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Minute)), Token: token, ProjectID: ti.projectID,
	}))
	cleaner := sessionhandoff.NewExpiredUnreadCleaner(sessionhandoff.NewPgxExpiredUnreadRepository(ti.conn), ti.blobs)
	_, err = cleaner.Clean(ctx)
	require.NoError(t, err)
	exists, err := ti.blobs.Exists(ctx, blobURL)
	require.NoError(t, err)
	require.True(t, exists, "cleaner must not steal an active consumer lease")

	require.NoError(t, fixtures.AgeSessionHandoffConsumerClaimFixture(ctx, testrepo.AgeSessionHandoffConsumerClaimFixtureParams{
		AttemptedAt: conv.ToPGTimestamptz(time.Now().Add(-sessionhandoff.ConsumerClaimLease - time.Minute)), Token: token, ProjectID: ti.projectID,
	}))
	_, err = cleaner.Clean(ctx)
	require.NoError(t, err)
	exists, err = ti.blobs.Exists(ctx, blobURL)
	require.NoError(t, err)
	require.False(t, exists, "expired consumer lease must become reclaimable")
}

func TestSessionHandoffRegistrySurvivesProjectDeletionAndPlatformReplays(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	key := "platform-" + uuid.NewString()
	ttl := sessionhandoff.DefaultHandoffTTL
	input := sessionhandoff.SessionHandoffMintInput{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, UserID: "user_project_delete",
		ChatID: uuid.New(), SessionID: uuid.NewString(), Content: "# project deletion",
		IdempotencyOperation: "platform_create", IdempotencyKey: key,
	}
	input.IdempotencyInputHash = sessionhandoff.PlatformIdempotencyInputHash(input.SessionID, ttl)
	minter := sessionhandoff.NewSessionHandoffMinter(testenv.NewLogger(t), ti.conn, ti.blobs, "https://example.test", audit.NewLogger())
	minted, err := minter.Mint(ctx, input)
	require.NoError(t, err)
	token := minted.URL[strings.LastIndex(minted.URL, "/")+1:]
	row, err := testrepo.New(ti.conn).GetSessionHandoffLinkFixture(ctx, token)
	require.NoError(t, err)
	blobURL, err := url.Parse(row.BlobUrl)
	require.NoError(t, err)

	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.HardDeleteProjectFixture(ctx, ti.projectID))
	replayed, err := minter.Replay(ctx, ti.orgID, input.UserID, input.IdempotencyOperation, key, input.IdempotencyInputHash)
	require.NoError(t, err)
	require.Equal(t, minted, replayed)

	require.NoError(t, fixtures.AgeSessionHandoffUploadRegistryFixture(ctx, testrepo.AgeSessionHandoffUploadRegistryFixtureParams{
		CreatedAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)), ProjectID: ti.projectID, BlobUrl: row.BlobUrl,
	}))
	_, _, err = sessionhandoff.NewUploadIntentCleaner(sessionhandoff.NewPgxUploadIntentRepository(ti.conn), ti.blobs).CleanAfter(ctx, time.Now().Add(-sessionhandoff.UploadIntentStaleAge), nil)
	require.NoError(t, err)
	exists, err := ti.blobs.Exists(ctx, blobURL)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestSessionHandoffRegistrySurvivesOrganizationDeletionAndCleansBlob(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	minter := sessionhandoff.NewSessionHandoffMinter(testenv.NewLogger(t), ti.conn, ti.blobs, "https://example.test", audit.NewLogger())
	minted, err := minter.Mint(ctx, sessionhandoff.SessionHandoffMintInput{
		OrganizationID: ti.orgID, ProjectID: ti.projectID, UserID: "user_org_delete",
		ChatID: uuid.New(), SessionID: uuid.NewString(), Content: "# org deletion",
	})
	require.NoError(t, err)
	token := minted.URL[strings.LastIndex(minted.URL, "/")+1:]
	row, err := testrepo.New(ti.conn).GetSessionHandoffLinkFixture(ctx, token)
	require.NoError(t, err)
	blobURL, err := url.Parse(row.BlobUrl)
	require.NoError(t, err)
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.AgeSessionHandoffUploadRegistryFixture(ctx, testrepo.AgeSessionHandoffUploadRegistryFixtureParams{
		CreatedAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)), ProjectID: ti.projectID, BlobUrl: row.BlobUrl,
	}))
	require.NoError(t, fixtures.HardDeleteOrganizationMetadataFixture(ctx, testrepo.HardDeleteOrganizationMetadataFixtureParams{OrganizationID: ti.orgID, ProjectID: ti.projectID}))

	cleaner := sessionhandoff.NewUploadIntentCleaner(sessionhandoff.NewPgxUploadIntentRepository(ti.conn), ti.blobs)
	_, _, err = cleaner.CleanAfter(ctx, time.Now().Add(-sessionhandoff.UploadIntentStaleAge), nil)
	require.NoError(t, err)
	exists, err := ti.blobs.Exists(ctx, blobURL)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestSessionHandoffOldReplicaUnkeyedInsertRemainsConsumable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	content := "# rolling deploy legacy handoff"
	token := strings.Repeat("ef", 32)
	writer, blobURL, err := ti.blobs.Write(ctx, "session-handoffs/"+token+".md", "text/markdown", int64(len(content)))
	require.NoError(t, err)
	_, err = io.WriteString(writer, content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	err = testrepo.New(ti.conn).InsertLegacySessionHandoffLinkFixture(ctx, testrepo.InsertLegacySessionHandoffLinkFixtureParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, SessionID: uuid.NewString(),
		Token: token, BlobUrl: blobURL.String(), CreatedByEmail: "legacy@example.test",
	})
	require.NoError(t, err, "old replicas must not be required to supply expanded columns")
	registry, err := testrepo.New(ti.conn).GetSessionHandoffUploadRegistryFixture(ctx, testrepo.GetSessionHandoffUploadRegistryFixtureParams{
		ProjectID: ti.projectID, BlobUrl: blobURL.String(),
	})
	require.NoError(t, err)
	require.Equal(t, "linked", registry.State)
	require.False(t, registry.ActorUserID.Valid, "legacy email must never become actor_user_id")
	require.Empty(t, registry.ObjectPath)

	response := serveHandoff(t, ti, token)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, content, response.Body.String())
}

func TestSessionHandoffRegistryBackfillCoversExistingLegacyLink(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	token := strings.Repeat("ab", 32)
	blobURL := "https://example.test/session-handoffs/" + token + ".md"
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.InsertLegacySessionHandoffLinkFixture(ctx, testrepo.InsertLegacySessionHandoffLinkFixtureParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, SessionID: uuid.NewString(), Token: token,
		BlobUrl: blobURL, CreatedByEmail: "legacy@example.test",
	}))
	deleted, err := fixtures.DeleteSessionHandoffUploadRegistryFixture(ctx, testrepo.DeleteSessionHandoffUploadRegistryFixtureParams{
		ProjectID: ti.projectID, BlobUrl: blobURL,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	require.NoError(t, fixtures.BackfillSessionHandoffUploadRegistryFixture(ctx, ti.projectID))
	registry, err := fixtures.GetSessionHandoffUploadRegistryFixture(ctx, testrepo.GetSessionHandoffUploadRegistryFixtureParams{
		ProjectID: ti.projectID, BlobUrl: blobURL,
	})
	require.NoError(t, err)
	require.Equal(t, ti.orgID, registry.OrganizationID)
	require.Equal(t, "linked", registry.State)
	require.False(t, registry.ActorUserID.Valid)
	require.Empty(t, registry.ObjectPath)
}

func TestSessionHandoffLinkTriggerLinksPendingIntentTransactionally(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	queries := repo.New(ti.conn)
	blobURL := "https://example.test/session-handoffs/" + uuid.NewString() + ".md"
	intentID, err := queries.CreateSessionHandoffUploadIntent(ctx, repo.CreateSessionHandoffUploadIntentParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, ActorUserID: "trigger-user",
		ObjectPath: "session-handoffs/pending.md", BlobUrl: blobURL,
	})
	require.NoError(t, err)
	registryParams := testrepo.GetSessionHandoffUploadRegistryFixtureParams{ProjectID: ti.projectID, BlobUrl: blobURL}
	registry, err := testrepo.New(ti.conn).GetSessionHandoffUploadRegistryFixture(ctx, registryParams)
	require.NoError(t, err)
	require.Equal(t, "pending", registry.State)

	insert := func(q *repo.Queries) {
		t.Helper()
		_, insertErr := q.InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
			ProjectID: ti.projectID, OrganizationID: ti.orgID, SessionID: uuid.NewString(),
			Token:   strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", ""),
			BlobUrl: blobURL, ActorUserID: conv.ToPGText("trigger-user"), CreatedByEmail: "",
			IdempotencyOperation: conv.ToPGTextEmpty(""), IdempotencyKey: conv.ToPGTextEmpty(""),
			IdempotencyInputHash: conv.ToPGTextEmpty(""), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Minute)),
			UploadIntentID: intentID,
		})
		require.NoError(t, insertErr)
	}

	tx := testenv.BeginTx(t, ctx, ti.conn)
	insert(repo.New(tx))
	inside, err := testrepo.New(tx).GetSessionHandoffUploadRegistryFixture(ctx, registryParams)
	require.NoError(t, err)
	require.Equal(t, "linked", inside.State)
	require.NoError(t, tx.Rollback(ctx))

	afterRollback, err := testrepo.New(ti.conn).GetSessionHandoffUploadRegistryFixture(ctx, registryParams)
	require.NoError(t, err)
	require.Equal(t, "pending", afterRollback.State)

	tx = testenv.BeginTx(t, ctx, ti.conn)
	insert(repo.New(tx))
	require.NoError(t, tx.Commit(ctx))
	afterCommit, err := testrepo.New(ti.conn).GetSessionHandoffUploadRegistryFixture(ctx, registryParams)
	require.NoError(t, err)
	require.Equal(t, "linked", afterCommit.State)
	require.Equal(t, "trigger-user", afterCommit.ActorUserID.String)
	require.Equal(t, "session-handoffs/pending.md", afterCommit.ObjectPath)
}

func TestSessionHandoffOldReplicaRegistryCleansAfterProjectDeletion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	content := "# legacy project deletion"
	token := strings.ReplaceAll(uuid.NewString(), "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
	writer, blobURL, err := ti.blobs.Write(ctx, "session-handoffs/"+token+".md", "text/markdown", int64(len(content)))
	require.NoError(t, err)
	_, err = io.WriteString(writer, content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.InsertLegacySessionHandoffLinkFixture(ctx, testrepo.InsertLegacySessionHandoffLinkFixtureParams{
		ProjectID: ti.projectID, OrganizationID: ti.orgID, SessionID: uuid.NewString(), Token: token,
		BlobUrl: blobURL.String(), CreatedByEmail: "legacy@example.test",
	}))
	require.NoError(t, fixtures.AgeSessionHandoffUploadRegistryFixture(ctx, testrepo.AgeSessionHandoffUploadRegistryFixtureParams{
		CreatedAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour)), ProjectID: ti.projectID, BlobUrl: blobURL.String(),
	}))
	require.NoError(t, fixtures.HardDeleteProjectFixture(ctx, ti.projectID))

	_, _, err = sessionhandoff.NewUploadIntentCleaner(sessionhandoff.NewPgxUploadIntentRepository(ti.conn), ti.blobs).CleanAfter(ctx, time.Now().Add(-sessionhandoff.UploadIntentStaleAge), nil)
	require.NoError(t, err)
	exists, err := ti.blobs.Exists(ctx, blobURL)
	require.NoError(t, err)
	require.False(t, exists)
}
