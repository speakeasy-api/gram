package usersessions_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// cimdDocServer hosts a Client ID Metadata Document at
// <TLS server>/oauth/client.json, mirroring the fixture the mcp package's
// authorize-time CIMD tests use. Tests reshape the served response through
// set before issuing requests; mu covers every mutable field because the
// handler runs on the server's own goroutine and a loopback round trip is
// not a synchronization edge the race detector recognizes.
type cimdDocServer struct {
	srv      *httptest.Server
	clientID string

	mu sync.Mutex

	// doc is the document body, encoded on every 200.
	doc map[string]any

	// etag, when non-empty, is served as the document's ETag and matched
	// against an incoming If-None-Match, which a match answers with 304.
	etag string

	// status, when non-zero, replaces every response with that error status,
	// standing in for a document host that has started failing.
	status int

	// onRequest, when set, runs on every document fetch before the response
	// is written. It lets a test mutate Gram's state at the exact point the
	// refresh handler is mid-fetch — after its purge committed, before its
	// persist runs — which is the window a concurrent revoke lands in.
	onRequest func()

	// requests counts document fetches. The refresh cooldown's whole value is
	// that a rejected refresh costs no outbound request, which is only
	// observable by asserting on this.
	requests atomic.Int64

	// conditionalRequests counts the subset of requests that carried an
	// If-None-Match header. A refresh purges the stored validator before
	// fetching, so this staying at zero is the acceptance criterion for
	// "refresh re-reads a full document body".
	conditionalRequests atomic.Int64
}

// set applies a mutation to the served response under the same lock the
// handler reads with.
func (ds *cimdDocServer) set(t *testing.T, mutate func(ds *cimdDocServer)) {
	t.Helper()

	ds.mu.Lock()
	defer ds.mu.Unlock()
	mutate(ds)
}

func (ds *cimdDocServer) certPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ds.srv.Certificate())
	return pool
}

func startCIMDDocServer(t *testing.T) *cimdDocServer {
	t.Helper()

	ds := &cimdDocServer{
		srv:                 nil,
		clientID:            "",
		mu:                  sync.Mutex{},
		doc:                 nil,
		etag:                "",
		status:              0,
		onRequest:           nil,
		requests:            atomic.Int64{},
		conditionalRequests: atomic.Int64{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/client.json", func(w http.ResponseWriter, r *http.Request) {
		ds.requests.Add(1)
		conditional := r.Header.Get("If-None-Match")
		if conditional != "" {
			ds.conditionalRequests.Add(1)
		}

		// Read under the lock, run outside it: the hook does database work
		// of its own and must not serialize against the response encoding.
		ds.mu.Lock()
		hook := ds.onRequest
		ds.mu.Unlock()
		if hook != nil {
			hook()
		}

		// Held for the whole response so the doc map cannot change mid
		// encode; requests in these tests are sequential, so contention is
		// not a concern.
		ds.mu.Lock()
		defer ds.mu.Unlock()

		if ds.status != 0 {
			http.Error(w, "document host failure", ds.status)
			return
		}

		if ds.etag != "" && conditional == ds.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if ds.etag != "" {
			w.Header().Set("ETag", ds.etag)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(ds.doc); err != nil {
			t.Errorf("encode cimd document: %v", err)
		}
	})
	ds.srv = httptest.NewTLSServer(mux)
	t.Cleanup(ds.srv.Close)

	ds.clientID = ds.srv.URL + "/oauth/client.json"
	ds.doc = map[string]any{
		"client_id":                  ds.clientID,
		"client_name":                "CIMD Refresh Client",
		"redirect_uris":              []any{"http://127.0.0.1:33418/callback"},
		"token_endpoint_auth_method": "none",
	}
	return ds
}

// newRefreshTestSetup builds a doc server plus a service whose guardian
// policy trusts the doc server's TLS certificate, creates an issuer, seeds a
// CIMD client row resolved from the doc server carrying a stored ETag, and
// enables FlagUserSessionCIMD for the caller's organization. The stored ETag
// is what makes the no-If-None-Match assertions meaningful: an ordinary
// revalidation of this row WOULD be conditional.
func newRefreshTestSetup(t *testing.T, issuerSlug string) (context.Context, *testInstance, *cimdDocServer, repo.UserSessionClient) {
	t.Helper()

	ds := startCIMDDocServer(t)
	ctx, ti := newTestServiceWithRevoker(t, nil, guardian.WithTLSRootCAs(ds.certPool()))

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 issuerSlug,
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	row, err := repo.New(ti.conn).UpsertUserSessionClientFromCIMD(ctx, repo.UpsertUserSessionClientFromCIMDParams{
		UserSessionIssuerID:  uuid.MustParse(issuer.ID),
		ClientID:             ds.clientID,
		ClientName:           "Stale Cached Name",
		RedirectUris:         []string{"http://127.0.0.1:33418/callback"},
		CacheTtlSeconds:      3600,
		ClientIDMetadataEtag: pgtype.Text{String: `"v1"`, Valid: true},
	})
	require.NoError(t, err)
	require.True(t, row.ClientIDMetadataEtag.Valid)

	enableCIMDFlag(t, ctx, ti)

	return ctx, ti, ds, row
}

// enableCIMDFlag turns FlagUserSessionCIMD on for the caller's organization,
// matching how the flag is targeted in PostHog (distinctID = org ID).
func enableCIMDFlag(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	ti.features.SetFlag(feature.FlagUserSessionCIMD, authCtx.ActiveOrganizationID, true)
}

// backdateFetchedAt ages a client row's last successful read past the refresh
// cooldown. Freshly seeded rows carry fetched_at = now and would otherwise be
// inside the window.
func backdateFetchedAt(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) {
	t.Helper()

	_, err := repo.New(ti.conn).SetUserSessionClientCIMDFetchedAt(ctx, repo.SetUserSessionClientCIMDFetchedAtParams{
		ID: id,
		FetchedAt: pgtype.Timestamptz{
			Time:             time.Now().Add(-time.Minute),
			InfinityModifier: 0,
			Valid:            true,
		},
	})
	require.NoError(t, err)
}

func refreshPayload(id string) *gen.RefreshUserSessionClientCIMDPayload {
	return &gen.RefreshUserSessionClientCIMDPayload{
		ID:               id,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

// A refresh must be an unconditional full read: the seeded row holds ETag
// "v1" and the host now serves that same validator, so a conditional request
// would be answered 304 and the stale extracted values would survive. The
// purge that precedes the fetch is what removes the validator.
func TestRefreshUserSessionClientCIMD(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, seeded := newRefreshTestSetup(t, "refresh-client-issuer")
	backdateFetchedAt(t, ctx, ti, seeded.ID)

	ds.set(t, func(ds *cimdDocServer) {
		ds.etag = `"v1"`
		ds.doc["client_name"] = "Republished Client Name"
	})

	auditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientCIMDRefresh)
	require.NoError(t, err)

	got, err := ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(seeded.ID.String()))
	require.NoError(t, err)

	require.Equal(t, int64(1), ds.requests.Load(), "refresh must fetch the document exactly once")
	require.Equal(t, int64(0), ds.conditionalRequests.Load(), "a purge must leave nothing to revalidate against; the read must carry no If-None-Match")

	require.Equal(t, seeded.ID.String(), got.ID)
	require.Equal(t, "Republished Client Name", got.ClientName, "the re-read document's extracted values must land")
	require.NotNil(t, got.ClientIDMetadataURI)
	require.Equal(t, ds.clientID, *got.ClientIDMetadataURI)
	require.NotNil(t, got.ClientIDMetadataEtag)
	require.Equal(t, `"v1"`, *got.ClientIDMetadataEtag, "the host's validator is stored again after the full read")
	require.NotNil(t, got.ClientIDMetadataFetchedAt)
	require.NotNil(t, got.ClientIDMetadataCacheExpiresAt)

	auditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientCIMDRefresh)
	require.NoError(t, err)
	require.Equal(t, auditBefore+1, auditAfter)
}

// Back-to-back refreshes of the same client must not produce back-to-back
// upstream fetches: the second call inside the cooldown is rejected before
// any request leaves Gram, and a client whose last read has aged past the
// window is allowed again.
func TestRefreshUserSessionClientCIMD_CooldownWindow(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, seeded := newRefreshTestSetup(t, "refresh-cooldown-issuer")
	backdateFetchedAt(t, ctx, ti, seeded.ID)

	_, err := ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(seeded.ID.String()))
	require.NoError(t, err)
	require.Equal(t, int64(1), ds.requests.Load())

	// The successful refresh stamped fetched_at = now, so an immediate second
	// attempt is inside the window.
	_, err = ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(seeded.ID.String()))
	requireOopsCode(t, err, oops.CodeRateLimitExceeded)
	require.Equal(t, int64(1), ds.requests.Load(), "a rejected refresh must cost no upstream request")

	backdateFetchedAt(t, ctx, ti, seeded.ID)

	_, err = ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(seeded.ID.String()))
	require.NoError(t, err)
	require.Equal(t, int64(2), ds.requests.Load(), "a refresh outside the window is allowed again")
}

// A refresh whose upstream read fails must still leave the row purged: the
// purge is the security-relevant half ("stop trusting this copy"), and it is
// recorded and committed before the fetch is attempted.
func TestRefreshUserSessionClientCIMD_FetchFailureStillPurges(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, seeded := newRefreshTestSetup(t, "refresh-fetchfail-issuer")
	backdateFetchedAt(t, ctx, ti, seeded.ID)

	ds.set(t, func(ds *cimdDocServer) {
		ds.status = http.StatusInternalServerError
	})

	auditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientCIMDRefresh)
	require.NoError(t, err)

	_, err = ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(seeded.ID.String()))
	requireOopsCode(t, err, oops.CodeGatewayError)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := repo.New(ti.conn).GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:        seeded.ID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.False(t, row.ClientIDMetadataCacheExpiresAt.Valid, "the purge must survive the failed fetch")
	require.False(t, row.ClientIDMetadataEtag.Valid, "the validator must stay cleared so the next read is unconditional")

	auditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientCIMDRefresh)
	require.NoError(t, err)
	require.Equal(t, auditBefore+1, auditAfter, "the cache mutation is recorded even when the re-read fails")
}

// A document that re-reads as invalid is a validation rejection the operator
// can act on, not a Gram fault: surfaced as invalid (422) with the
// client-safe description, distinct from the unreachable-host 502.
func TestRefreshUserSessionClientCIMD_InvalidDocumentRejected(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, seeded := newRefreshTestSetup(t, "refresh-invaliddoc-issuer")
	backdateFetchedAt(t, ctx, ti, seeded.ID)

	ds.set(t, func(ds *cimdDocServer) {
		delete(ds.doc, "redirect_uris")
	})

	_, err := ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(seeded.ID.String()))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Equal(t, int64(1), ds.requests.Load())
}

// A client revoked while the refresh's document fetch is in flight must stay
// revoked. The persist step is an id-scoped update precisely because the
// authorize path's (issuer, client_id) upsert conflicts only against live
// rows — against the just-revoked row it would take the INSERT branch and
// silently resurrect the client with a fresh id. The doc server's onRequest
// hook lands the revoke inside the exact race window: after the refresh's
// purge committed, before its persist runs.
func TestRefreshUserSessionClientCIMD_RevokedMidFetchStaysRevoked(t *testing.T) {
	t.Parallel()

	ctx, ti, ds, seeded := newRefreshTestSetup(t, "refresh-revoke-race-issuer")
	backdateFetchedAt(t, ctx, ti, seeded.ID)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	ds.set(t, func(ds *cimdDocServer) {
		ds.onRequest = func() {
			// t.Errorf rather than require: this runs on the doc server's
			// goroutine, where FailNow must not be called.
			if _, err := repo.New(ti.conn).RevokeUserSessionClient(ctx, repo.RevokeUserSessionClientParams{
				ID:        seeded.ID,
				ProjectID: projectID,
			}); err != nil {
				t.Errorf("revoke client mid-fetch: %v", err)
			}
		}
	})

	_, err := ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(seeded.ID.String()))
	requireOopsCode(t, err, oops.CodeNotFound)

	// The revocation must hold: no live row may exist for this client_id,
	// under the original id or any other.
	_, err = repo.New(ti.conn).GetUserSessionClientByClientID(ctx, repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: seeded.UserSessionIssuerID,
		ClientID:            seeded.ClientID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "refresh must not resurrect a client revoked during its fetch")
}

// A DCR client has no metadata document; the refresh endpoint refuses rather
// than treating it as refreshable.
func TestRefreshUserSessionClientCIMD_DCRClientRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "refresh-dcr-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "refresh-dcr-client")
	require.NoError(t, err)

	enableCIMDFlag(t, ctx, ti)

	_, err = ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(client.ID.String()))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// The endpoint carries the same organization flag gate as the /authorize
// resolve path: an org whose flag is off cannot make Gram fetch documents,
// and the rejection costs no upstream request.
func TestRefreshUserSessionClientCIMD_FlagDisabled(t *testing.T) {
	t.Parallel()

	ds := startCIMDDocServer(t)
	ctx, ti := newTestServiceWithRevoker(t, nil, guardian.WithTLSRootCAs(ds.certPool()))

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "refresh-flagoff-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedCimdUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), ds.clientID)
	require.NoError(t, err)
	backdateFetchedAt(t, ctx, ti, client.ID)

	_, err = ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(client.ID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Equal(t, int64(0), ds.requests.Load())
}

func TestRefreshUserSessionClientCIMD_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(uuid.NewString()))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRefreshUserSessionClientCIMD_BadID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload("not-a-uuid"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestRefreshUserSessionClientCIMD_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "refresh-rbac-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedCimdUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "https://client.example.com/oauth/client.json")
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Read-only on the project; refresh needs write.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	_, err = ti.service.RefreshUserSessionClientCIMD(ctx, refreshPayload(client.ID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}
