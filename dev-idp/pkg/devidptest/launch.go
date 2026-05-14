// Package devidptest spins up a real dev-idp HTTP server inside a test.
//
// Launch wires bootstrap.Open + keystore.New + the oauth2 / oauth2-1 (and
// optionally mock-workos) mode handlers under an httptest.NewServer,
// returning an Instance with the addressable issuer URLs, a *sql.DB handle,
// a *repo.Queries for direct sqlc seeding, and helpers for fetching
// authorization-server metadata and seeding refresh tokens.
//
// Each Launch is fully isolated: a fresh in-memory SQLite database, a fresh
// httptest port, and (by default) a process-wide RSA key shared via
// sync.Once. Tests that need a distinct signing key can pass LaunchOpts.Key.
package devidptest

import (
	"crypto/rsa"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/mockworkos"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/oauth2"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/oauth21"
	"github.com/speakeasy-api/gram/plog"
)

const (
	defaultUserEmail       = "test@devidptest.local"
	defaultUserDisplayName = "Test User"
)

// Mode discriminator strings persisted by dev-idp on its auth_codes,
// tokens, and current_users rows.
const (
	// OAuth20Mode is the discriminator for OAuth 2.0 mode rows.
	OAuth20Mode = oauth2.Mode

	// OAuth21Mode is the discriminator for OAuth 2.1 mode rows.
	OAuth21Mode = oauth21.Mode

	// MockWorkosMode is the discriminator for mock-workos mode rows.
	MockWorkosMode = mockworkos.Mode
)

// Instance is a running dev-idp server with everything tests need to drive
// OAuth flows against it: per-mode endpoint URLs, the underlying *sql.DB and
// *repo.Queries handles, and helpers for seeding fixture rows.
type Instance struct {
	// Issuer is the externally addressable base URL of the running server,
	// without any mode prefix (e.g. "http://127.0.0.1:38291").
	Issuer string

	// OAuth20URL is the issuer URL of the OAuth 2.0 mode handler
	// (Issuer + "/oauth2"). Use this wherever a Gram toolset or
	// external_oauth_server_metadata row references the upstream OAuth
	// 2.0 authorization server.
	OAuth20URL string

	// OAuth21URL is the issuer URL of the OAuth 2.1 mode handler
	// (Issuer + "/oauth2-1"). Use this wherever a Gram toolset or
	// external_oauth_server_metadata row references the upstream OAuth
	// 2.1 authorization server.
	OAuth21URL string

	// MockWorkosURL is the prefix mounted for the mock-workos mode
	// (Issuer + "/mock-workos"). Empty when
	// LaunchOpts.EnableMockWorkos is false.
	MockWorkosURL string

	// DB is the dev-idp's in-memory SQLite handle. Most tests should
	// reach for Repo instead; DB is exposed for tests that need to drop
	// to raw database/sql operations not covered by the sqlc surface.
	DB *sql.DB

	// Repo is the sqlc query helper bound to DB. Tests use this for
	// direct fixture seeding (CreateUser, CreateOrganization,
	// CreateMembership, CreateToken, etc.).
	Repo *repo.Queries

	// DefaultUser is auto-seeded by Launch and is the subject_ref for
	// the per-mode current_users rows, so the OAuth flow handlers can
	// reach a current user without a separate fixture step. Tests that
	// drive CreateRefreshToken (or any other tokens-table seed) typically
	// pass DefaultUser.ID as the row's user_id.
	DefaultUser repo.User

	server *httptest.Server
	rsaKey *rsa.PrivateKey
}

// LaunchOpts configures Launch. The zero value is valid: oauth2 + oauth2-1
// mounted, mock-workos disabled, shared package-level RSA key.
type LaunchOpts struct {
	// EnableMockWorkos mounts the mock-workos mode under
	// Instance.MockWorkosURL. Disabled by default — most OAuth flow
	// tests don't need it.
	EnableMockWorkos bool

	// Key, when non-nil, overrides the shared package-level RSA key. Use
	// this for tests that need a distinct signing key (JWKS rotation,
	// kid-mismatch). Most tests should leave this nil.
	Key *rsa.PrivateKey
}

// Launch starts a fresh dev-idp HTTP server on a random loopback port. The
// returned Instance is usable until the test ends; t.Cleanup tears down the
// httptest server before closing the SQLite database.
func Launch(t *testing.T, opts LaunchOpts) *Instance {
	t.Helper()

	ctx := t.Context()
	logger := plog.NewLogger(io.Discard)
	var tp trace.TracerProvider = tracenoop.NewTracerProvider()

	db, err := bootstrap.Open(ctx, config.DB{Mode: config.DBModeMemory})
	require.NoError(t, err, "open dev-idp in-memory sqlite")

	// httptest.Server.Close drains in-flight requests but does not block
	// new ones, so it must run before db.Close to avoid handlers hitting a
	// closed DB. t.Cleanup runs LIFO: register Close first, then the DB
	// close, so the DB close happens last.
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Logf("close dev-idp sqlite: %v", cerr)
		}
	})

	rsaKey := opts.Key
	var pemBytes []byte
	if rsaKey == nil {
		rsaKey, pemBytes = sharedKey(t)
	} else {
		pemBytes, err = encodeRSAPrivateKey(rsaKey)
		require.NoError(t, err, "encode caller-supplied rsa key")
	}

	ks, err := keystore.New(pemBytes, logger)
	require.NoError(t, err, "init dev-idp keystore")

	// Use NewUnstartedServer so we can read the bound listener address
	// before constructing handlers — the mode handlers stamp the issuer
	// URL into their JWT claims and discovery documents at construction
	// time, so they must know the public URL up front.
	outer := http.NewServeMux()
	server := httptest.NewUnstartedServer(outer)
	pubURL := "http://" + server.Listener.Addr().String()

	oauth21H := oauth21.NewHandler(oauth21.Config{ExternalURL: pubURL}, ks, logger, tp, db)
	outer.Handle(oauth21.Prefix+"/", http.StripPrefix(oauth21.Prefix, oauth21H.Handler()))

	oauth2H := oauth2.NewHandler(oauth2.Config{ExternalURL: pubURL}, ks, logger, tp, db)
	outer.Handle(oauth2.Prefix+"/", http.StripPrefix(oauth2.Prefix, oauth2H.Handler()))

	var mockWorkosURL string
	if opts.EnableMockWorkos {
		mwH := mockworkos.NewHandler(logger, tp, db)
		outer.Handle(mockworkos.Prefix+"/", http.StripPrefix(mockworkos.Prefix, mwH.Handler()))
		mockWorkosURL = pubURL + mockworkos.Prefix
	}

	server.Start()
	t.Cleanup(server.Close)

	queries := repo.New(db)

	user, err := queries.CreateUser(ctx, repo.CreateUserParams{
		ID:           uuid.New(),
		Email:        defaultUserEmail,
		DisplayName:  defaultUserDisplayName,
		PhotoUrl:     sql.NullString{},
		GithubHandle: sql.NullString{},
		Admin:        false,
		Whitelisted:  true,
	})
	require.NoError(t, err, "seed default user")

	for _, mode := range currentUserModes(opts.EnableMockWorkos) {
		_, err := queries.UpsertCurrentUser(ctx, repo.UpsertCurrentUserParams{
			Mode:       mode,
			SubjectRef: user.ID.String(),
			Ts:         time.Now(),
		})
		require.NoError(t, err, "upsert default current_users for %s", mode)
	}

	return &Instance{
		Issuer:        pubURL,
		OAuth20URL:    pubURL + oauth2.Prefix,
		OAuth21URL:    pubURL + oauth21.Prefix,
		MockWorkosURL: mockWorkosURL,
		DB:            db,
		Repo:          queries,
		DefaultUser:   user,
		server:        server,
		rsaKey:        rsaKey,
	}
}

// SigningKey returns the RSA private key the dev-idp signs id_tokens with.
// Tests that need to verify signatures or mint their own JWTs can use this.
func (i *Instance) SigningKey() *rsa.PrivateKey { return i.rsaKey }

// OAuth21Metadata fetches the dev-idp's RFC 8414 authorization-server
// metadata for the oauth2-1 mode. The bytes are suitable for storing in the
// Gram-side external_oauth_server_metadata table.
func (i *Instance) OAuth21Metadata(t *testing.T) []byte {
	t.Helper()
	return fetchMetadata(t, i.OAuth21URL)
}

// OAuth20Metadata fetches the dev-idp's RFC 8414 authorization-server
// metadata for the oauth2 mode.
func (i *Instance) OAuth20Metadata(t *testing.T) []byte {
	t.Helper()
	return fetchMetadata(t, i.OAuth20URL)
}

func fetchMetadata(t *testing.T, modeURL string) []byte {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		strings.TrimRight(modeURL, "/")+"/.well-known/oauth-authorization-server", nil)
	require.NoError(t, err, "build metadata request")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "fetch metadata")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode, "metadata status")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read metadata body")
	return body
}

// currentUserModes lists the per-mode discriminator strings whose
// current_users rows need to point at Instance.DefaultUser. Mirrors the
// modes Launch actually mounts.
func currentUserModes(enableMockWorkos bool) []string {
	modes := []string{oauth21.Mode, oauth2.Mode}
	if enableMockWorkos {
		modes = append(modes, mockworkos.Mode)
	}
	return modes
}
