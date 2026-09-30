package oktaapplications_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	idprepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oktacredentials"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
	"github.com/stretchr/testify/require"
)

func tokenConfig(t *testing.T, bc *basicConnection) okta.Config {
	t.Helper()
	row, err := idprepo.New(bc.conn).GetManagedClient(t.Context(), idprepo.GetManagedClientParams{OrganizationID: conv.ToPGText(bc.orgID), IdentityProviderConnectionID: conv.ToNullUUID(bc.connectionID)})
	require.NoError(t, err)
	return okta.Config{AudienceFormat: string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint), ClientID: row.RemoteSessionClient.ClientID, RemoteSessionClientID: row.RemoteSessionClient.ID, OrganizationID: bc.orgID, AuthMethod: remotesessions.TokenEndpointAuthMethodBasic, ClientSecretEncrypted: row.RemoteSessionClient.ClientSecretEncrypted.String, Credentials: oktacredentials.Provider{DB: bc.conn, ConnectionID: bc.connectionID}}
}

func tokenFactory(t *testing.T, bc *basicConnection) okta.ClientFactory {
	t.Helper()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return okta.NewClientFactory(testenv.NewLogger(t), policy, nil, bc.enc)
}

func TestWorkerTokenCredentialLivenessAndBinding(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	bc := newBasicConnection(t, ctx)
	var bearer atomic.Bool
	var exchanges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/oauth2/v1/token" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		exchanges.Add(1)
		kind := "DPoP"
		if bearer.Load() {
			kind = "Bearer"
		}
		_, _ = fmt.Fprintf(w, `{"access_token":"test-token","token_type":%q,"expires_in":3600,"scope":"okta.apps.read okta.users.read okta.groups.read"}`, kind)
	}))
	defer srv.Close()
	cfg := tokenConfig(t, bc)
	cfg.OrgURL = srv.URL
	factory := tokenFactory(t, bc)
	client, err := factory.Client(cfg)
	require.NoError(t, err)
	_, err = client.ListApps(ctx, okta.ListAppsRequest{})
	require.NoError(t, err)
	require.EqualValues(t, 1, exchanges.Load())

	// A new factory simulates a worker restart. Deliberately reuse the stale
	// unpinned config: the database observation, not this snapshot, is authoritative.
	bearer.Store(true)
	restarted, err := tokenFactory(t, bc).Client(cfg)
	require.NoError(t, err)
	_, err = restarted.ListApps(ctx, okta.ListAppsRequest{})
	require.ErrorContains(t, err, "not accepted for client_secret_basic")

	// This client was constructed before withdrawal but has never acquired a token.
	stale, err := tokenFactory(t, bc).Client(cfg)
	require.NoError(t, err)
	_, err = idprepo.New(bc.conn).ClearManagedClientSecret(ctx, idprepo.ClearManagedClientSecretParams{ID: cfg.RemoteSessionClientID, OrganizationID: conv.ToPGText(bc.orgID), IdentityProviderConnectionID: conv.ToNullUUID(bc.connectionID)})
	require.NoError(t, err)
	before := exchanges.Load()
	_, err = stale.ListApps(ctx, okta.ListAppsRequest{})
	require.ErrorContains(t, err, "credential revoked")
	require.Equal(t, before, exchanges.Load(), "revoked snapshot cannot contact the token endpoint")
}

func TestTokenCredentialLeaseSerializesWithdrawal(t *testing.T) {
	t.Parallel()
	bc := newBasicConnection(t, t.Context())
	cfg := tokenConfig(t, bc)
	lease, err := cfg.Credentials.Acquire(t.Context(), cfg)
	require.NoError(t, err)
	defer lease.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = idprepo.New(bc.conn).ClearManagedClientSecret(ctx, idprepo.ClearManagedClientSecretParams{ID: cfg.RemoteSessionClientID, OrganizationID: conv.ToPGText(bc.orgID), IdentityProviderConnectionID: conv.ToNullUUID(bc.connectionID)})
	require.Error(t, err, "withdrawal must wait until token exchange finishes")
	require.NoError(t, lease.Observe(t.Context(), true))
	lease.Close()
	_, err = idprepo.New(bc.conn).ClearManagedClientSecret(t.Context(), idprepo.ClearManagedClientSecretParams{ID: cfg.RemoteSessionClientID, OrganizationID: conv.ToPGText(bc.orgID), IdentityProviderConnectionID: conv.ToNullUUID(bc.connectionID)})
	require.NoError(t, err)
}

func TestTokenCredentialVerificationUsesUncommittedReplacement(t *testing.T) {
	t.Parallel()
	bc := newBasicConnection(t, t.Context())
	cfg := tokenConfig(t, bc)
	tx := testenv.BeginTx(t, t.Context(), bc.conn)
	q := idprepo.New(tx)
	_, err := q.LockOktaIdentityProviderConnection(t.Context(), idprepo.LockOktaIdentityProviderConnectionParams{ID: bc.connectionID, OrganizationID: bc.orgID})
	require.NoError(t, err)
	replacement, err := bc.enc.Encrypt([]byte("replacement-secret"))
	require.NoError(t, err)
	_, err = q.SetManagedClientSecret(t.Context(), idprepo.SetManagedClientSecretParams{ID: cfg.RemoteSessionClientID, OrganizationID: conv.ToPGText(bc.orgID), IdentityProviderConnectionID: conv.ToNullUUID(bc.connectionID), ClientSecretEncrypted: conv.ToPGText(replacement)})
	require.NoError(t, err)
	cfg.Credentials = oktacredentials.Provider{Tx: tx, ConnectionID: bc.connectionID}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	lease, err := cfg.Credentials.Acquire(ctx, cfg)
	require.NoError(t, err)
	defer lease.Close()
	require.Equal(t, replacement, lease.EncryptedSecret())
	require.NoError(t, lease.Observe(ctx, true))
	require.NoError(t, tx.Commit(ctx), "the provider must not commit the caller's transaction")
}

func TestTokenCredentialWaiterObservesCommittedBinding(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	bc := newBasicConnection(t, ctx)
	cfg := tokenConfig(t, bc)
	first, err := cfg.Credentials.Acquire(ctx, cfg)
	require.NoError(t, err)
	defer first.Close()
	require.False(t, first.RequireDPoP())

	// Know the waiter's backend PID so the first exchange cannot commit until
	// PostgreSQL confirms that the second locking statement is actually waiting.
	tx := testenv.BeginTx(t, ctx, bc.conn)
	pid := tx.Conn().PgConn().PID()
	type acquisition struct {
		lease okta.CredentialLease
		err   error
	}
	result := make(chan acquisition, 1)
	go func() {
		p := oktacredentials.Provider{Tx: tx, ConnectionID: bc.connectionID}
		l, acquireErr := p.Acquire(ctx, cfg)
		result <- acquisition{lease: l, err: acquireErr}
	}()
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(bc.conn).IsLifecycleBackendBlockedFixture(ctx, int32(pid))
		return err == nil && blocked
	}, 3*time.Second, 10*time.Millisecond, "second acquisition must be waiting before pin commits")
	require.NoError(t, first.Observe(ctx, true))
	select {
	case got := <-result:
		require.NoError(t, got.err)
		defer got.lease.Close()
		require.True(t, got.lease.RequireDPoP(), "waiter must read the subtype after the lock wait")
	case <-ctx.Done():
		t.Fatal("credential waiter did not finish")
	}
}

func TestVerificationWriteCannotClearObservedBinding(t *testing.T) {
	t.Parallel()
	bc := newBasicConnection(t, t.Context())
	cfg := tokenConfig(t, bc)
	tx := testenv.BeginTx(t, t.Context(), bc.conn)
	provider := oktacredentials.Provider{Tx: tx, ConnectionID: bc.connectionID}
	lease, err := provider.Acquire(t.Context(), cfg)
	require.NoError(t, err)
	defer lease.Close()
	// A later confirmReads token exchange can observe binding after the initial
	// scope-verification token was Bearer. Its stale false outcome must not win.
	require.NoError(t, lease.Observe(t.Context(), true))
	row, err := idprepo.New(tx).UpdateOktaIdentityProviderConnectionVerification(t.Context(), idprepo.UpdateOktaIdentityProviderConnectionVerificationParams{IdentityProviderConnectionID: bc.connectionID, OrganizationID: bc.orgID, DpopRequired: false, PreserveDpop: true, OwnershipClaimed: true, GrantedScopes: []string{"okta.apps.read"}})
	require.NoError(t, err)
	require.True(t, row.DpopRequired)
	require.NoError(t, tx.Commit(t.Context()))
}
