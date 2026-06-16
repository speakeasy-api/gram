// tokenservice_resolve_test.go covers ResolveAccessTokens, the multi-client
// MCP-runtime resolver that returns one upstream token per
// remote_session_issuer linked to a user_session_issuer (keyed by
// remote_session_issuer_id), and its "any attached session missing/invalid →
// ErrNoValidToken" rule.
//
// The single-client happy path is the only multiplicity reachable today: the
// remote_session_client_user_session_issuers one_per_issuer unique index still
// caps a user_session_issuer at one client. The map-with-many-entries and the
// per-issuer uniqueness invariant become exercisable once AIS-137 drops that
// index.

package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newResolveManager(t *testing.T, conn *pgxpool.Pool, enc *encryption.Client) *remotesessions.ChallengeManager {
	t.Helper()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	return remotesessions.NewChallengeManager(
		testenv.NewLogger(t),
		conn,
		enc,
		policy,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
	)
}

// seedActiveClient creates a remote_session_issuer + remote_session_client
// (attached to userIssuerID through the join table) and returns the client id
// and its remote_session_issuer id.
func seedActiveClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, userIssuerID uuid.UUID, slug string) (clientID, remoteIssuerID uuid.UUID) {
	t.Helper()

	q := repo.New(conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		Slug:                              slug,
		Issuer:                            "https://issuer.example.com/" + slug,
		AuthorizationEndpoint:             conv.ToPGText("https://issuer.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://issuer.example.com/token"),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(projectID),
		RemoteSessionIssuerID:   issuer.ID,
		UserSessionIssuerID:     conv.ToNullUUID(userIssuerID),
		ClientID:                "cid-" + slug,
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        conv.ToPGTimestamptz(time.Now()),
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_post"),
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	err = q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuerID,
	})
	require.NoError(t, err)

	return client.ID, issuer.ID
}

func TestResolveAccessTokens_SingleClientHappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	mgr := newResolveManager(t, ti.conn, enc)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-resolve-happy")
	clientID, remoteIssuerID := seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, "rsi-resolve-happy")

	subject := urn.NewUserSubject("resolve-happy-subject")
	accessEnc, err := enc.Encrypt([]byte("upstream-access-token"))
	require.NoError(t, err)
	_, err = repo.New(ti.conn).InsertRemoteSession(ctx, repo.InsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, userIssuerID, subject)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]string{remoteIssuerID: "upstream-access-token"}, tokens)
}

func TestResolveAccessTokens_NoClientsReturnsNil(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-resolve-empty")
	subject := urn.NewUserSubject("resolve-empty-subject")

	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, userIssuerID, subject)
	require.NoError(t, err)
	require.Nil(t, tokens)
}

func TestResolveAccessTokens_MissingSessionReturnsErrNoValidToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "usi-resolve-missing")
	// Client bound, but the subject has never linked an upstream session.
	seedActiveClient(t, ctx, ti.conn, *authCtx.ProjectID, userIssuerID, "rsi-resolve-missing")

	subject := urn.NewUserSubject("resolve-missing-subject")
	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, userIssuerID, subject)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	require.Nil(t, tokens)
}
