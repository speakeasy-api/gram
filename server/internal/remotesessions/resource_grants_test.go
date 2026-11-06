package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// This DDL runs only in a test's private cloned database. It tests the
// compatibility application before the separately gated migration ships.
func expandResourceGrantIndexes(t *testing.T, ctx context.Context, conn *pgxpool.Pool) {
	t.Helper()
	_, err := conn.Exec(ctx, `
 DROP INDEX IF EXISTS remote_sessions_subject_client_key;
 DROP INDEX IF EXISTS remote_sessions_subject_client_issuer_key;
 CREATE UNIQUE INDEX IF NOT EXISTS remote_sessions_subject_client_resource_key
 ON remote_sessions (subject_urn, remote_session_client_id, resource) NULLS NOT DISTINCT WHERE deleted IS FALSE;
 CREATE UNIQUE INDEX IF NOT EXISTS remote_sessions_subject_client_issuer_resource_key
 ON remote_sessions (subject_urn, remote_session_client_id, user_session_issuer_id, resource) NULLS NOT DISTINCT WHERE deleted IS FALSE;`)
	require.NoError(t, err)
}

func TestRemoteSessionResourceGrants_Lifecycle(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "resource-grants")
	expandResourceGrantIndexes(t, ctx, f.ti.conn)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	subject := urn.NewUserSubject(auth.UserID)
	q := repo.New(f.ti.conn)
	params := repo.UpsertRemoteSessionParams{
		SubjectUrn: subject, UserSessionIssuerID: f.signInA, RemoteSessionClientID: f.clientID,
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{"read"},
	}
	consent := func(resource, token string) repo.RemoteSession {
		t.Helper()
		encrypted, err := f.enc.Encrypt([]byte(token))
		require.NoError(t, err)
		params.Resource = conv.ToPGTextEmpty(resource)
		params.Scopes = []string{token + "-scope"}
		params.AccessTokenEncrypted = encrypted
		row, err := q.UpsertRemoteSession(ctx, params)
		require.NoError(t, err)
		return row
	}
	a := consent("https://resource-a.example/mcp", "access-a")
	b := consent("https://resource-b.example/mcp", "access-b")
	require.NotEqual(t, a.ID, b.ID, "one human and one registration retain two audience-qualified grants")
	require.EqualValues(t, 1, a.GrantGeneration)
	require.EqualValues(t, 1, b.GrantGeneration)
	assertToken := func(resource, want string) {
		t.Helper()
		req := f.request(subject, f.clientID)
		req.Resource = resource
		tokens, err := f.mgr.ResolveAuthClientAccessTokens(ctx, req)
		require.NoError(t, err)
		require.Equal(t, want, tokens[f.issuerID].Token)
		require.Equal(t, resource, tokens[f.issuerID].Resource)
		reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, req)
		require.NoError(t, err)
		require.False(t, reconnect)
	}
	assertToken(a.Resource.String, "access-a")
	assertToken(b.Resource.String, "access-b")
	for _, resource := range []string{a.Resource.String, b.Resource.String} {
		states, err := f.mgr.AuthClientRemoteSessionStatusesForResource(ctx, subject, f.projectID, f.organizationID, []uuid.UUID{f.clientID}, resource)
		require.NoError(t, err)
		require.Equal(t, resource, states[f.clientID].Resource)
		expectedScope := "access-a-scope"
		if resource == b.Resource.String {
			expectedScope = "access-b-scope"
		}
		require.Equal(t, []string{expectedScope}, states[f.clientID].Scopes)
	}
	states, err := f.mgr.AuthClientRemoteSessionStatuses(ctx, subject, f.projectID, f.organizationID, []uuid.UUID{f.clientID})
	require.NoError(t, err)
	require.Empty(t, states, "registration-only status must not choose a resource arbitrarily")
	_, err = q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: f.clientID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.GetTenantUserRemoteSessionForClient(ctx, repo.GetTenantUserRemoteSessionForClientParams{SubjectUrn: subject, RemoteSessionClientID: f.clientID, ProjectID: f.projectID, OrganizationID: "another-organization", Resource: a.Resource.String})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: urn.NewUserSubject("another-user"), RemoteSessionClientID: f.clientID, Resource: a.Resource.String})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	a2 := consent(a.Resource.String, "reconnected-a")
	require.Equal(t, a.ID, a2.ID)
	require.Equal(t, a.GrantGeneration+1, a2.GrantGeneration)
	assertToken(b.Resource.String, "access-b")
	// A stale writer for A must not update B even when it presents B's CAS value.
	_, err = q.UpdateRemoteSessionTokensIfUnchanged(ctx, repo.UpdateRemoteSessionTokensIfUnchangedParams{
		ID: a.ID, SubjectUrn: subject, RemoteSessionClientID: f.clientID, ExpectedUpdatedAt: b.UpdatedAt,
		AccessTokenEncrypted: "must-not-land", Scopes: []string{},
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	assertToken(b.Resource.String, "access-b")
	_, err = q.RevokeRemoteSession(ctx, repo.RevokeRemoteSessionParams{ID: a.ID, ProjectID: f.projectID, OrganizationID: f.organizationID})
	require.NoError(t, err)
	req := f.request(subject, f.clientID)
	req.Resource = a.Resource.String
	reconnect, err := f.mgr.AuthClientNeedsReconnect(ctx, req)
	require.NoError(t, err)
	require.True(t, reconnect)
	assertToken(b.Resource.String, "access-b")
	a3 := consent(a.Resource.String, "new-a")
	require.NotEqual(t, a.ID, a3.ID)
	require.EqualValues(t, 1, a3.GrantGeneration)
	assertToken(a.Resource.String, "new-a")
	assertToken(b.Resource.String, "access-b")
	// A legacy unbound grant coexists but cannot hide either exact grant.
	legacy := consent("", "legacy")
	sameLegacy := consent("", "legacy-reconnected")
	require.Equal(t, legacy.ID, sameLegacy.ID)
	require.Equal(t, legacy.GrantGeneration+1, sameLegacy.GrantGeneration)
	assertToken(a.Resource.String, "new-a")
	assertToken(b.Resource.String, "access-b")
}

func TestRemoteSessionResourceGrants_OldSchemaMigrationGatePreservesA(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "resource-grants-old")
	// Remain valid when this test is run after the later index migration too.
	_, err := f.ti.conn.Exec(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS remote_sessions_subject_client_key ON remote_sessions (subject_urn, remote_session_client_id) WHERE deleted IS FALSE`)
	require.NoError(t, err)
	q := repo.New(f.ti.conn)
	subject := urn.NewUserSubject("old-schema-user")
	params := repo.UpsertRemoteSessionParams{SubjectUrn: subject, UserSessionIssuerID: f.signInA, RemoteSessionClientID: f.clientID, AccessTokenEncrypted: "original-a", Scopes: []string{"read-a"}, Resource: conv.ToPGText("https://a.example/mcp")}
	a, err := q.UpsertRemoteSession(ctx, params)
	require.NoError(t, err)
	params.Resource = conv.ToPGText("https://b.example/mcp")
	params.AccessTokenEncrypted = "must-not-replace-a"
	params.Scopes = []string{"read-b"}
	_, err = q.UpsertRemoteSession(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	retained, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: a.ID, SubjectUrn: subject, RemoteSessionClientID: f.clientID})
	require.NoError(t, err)
	require.Equal(t, a, retained)
	params.Resource = a.Resource
	params.AccessTokenEncrypted = "reconnected-a"
	reconnected, err := q.UpsertRemoteSession(ctx, params)
	require.NoError(t, err)
	require.Equal(t, a.ID, reconnected.ID)
	require.Equal(t, a.GrantGeneration+1, reconnected.GrantGeneration)
}

func TestRemoteSessionResourceGrants_RefreshOnlySelectedAudience(t *testing.T) {
	t.Parallel()
	var spy upstreamSpy
	const resourceA = "https://a.example/mcp"
	const resourceB = "https://b.example/mcp"
	ctx, mgr, ti, clientID, subject := setupRefreshFixtureWithHandler(t, "two-resource-refresh", conv.ToPGText(resourceA), spyRefreshHandler(&spy))
	expandResourceGrantIndexes(t, ctx, ti.conn)
	q := repo.New(ti.conn)
	a, err := q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientID, Resource: resourceA})
	require.NoError(t, err)
	encrypted, err := testenv.NewEncryptionClient(t).Encrypt([]byte("b-original"))
	require.NoError(t, err)
	b, err := q.UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{SubjectUrn: subject, UserSessionIssuerID: a.UserSessionIssuerID, RemoteSessionClientID: clientID, AccessTokenEncrypted: encrypted, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Resource: conv.ToPGText(resourceB), Scopes: []string{}})
	require.NoError(t, err)
	token, err := mgr.ResolveAccessToken(ctx, remotesessions.RefreshAttribution{}, clientID, subject, resourceA)
	require.NoError(t, err)
	require.Equal(t, "refreshed-access", token)
	require.Equal(t, resourceA, spy.form.Get("resource"))
	retainedB, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: b.ID, SubjectUrn: subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	require.Equal(t, b, retainedB)
	refreshedA, err := q.GetActiveRemoteSessionByID(ctx, repo.GetActiveRemoteSessionByIDParams{ID: a.ID, SubjectUrn: subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	require.Equal(t, a.GrantGeneration, refreshedA.GrantGeneration)
	require.True(t, refreshedA.UpdatedAt.Time.After(a.UpdatedAt.Time))
}

func TestRemoteSessionResourceGrants_LegacyNormalizationAndDuplicateRefusal(t *testing.T) {
	t.Parallel()
	ctx, f := newAuthClientTokenFixture(t, "resource-normalization")
	expandResourceGrantIndexes(t, ctx, f.ti.conn)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	subject := urn.NewUserSubject(auth.UserID)
	encrypted, err := f.enc.Encrypt([]byte("audience-token"))
	require.NoError(t, err)
	q := repo.New(f.ti.conn)
	params := repo.UpsertRemoteSessionParams{SubjectUrn: subject, UserSessionIssuerID: f.signInA, RemoteSessionClientID: f.clientID, Resource: conv.ToPGText("https://audience.example/mcp/"), AccessTokenEncrypted: encrypted, AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), Scopes: []string{}}
	original, err := q.UpsertRemoteSession(ctx, params)
	require.NoError(t, err)
	req := f.request(subject, f.clientID)
	req.Resource = "https://audience.example/mcp"
	tokens, err := f.mgr.ResolveAuthClientAccessTokens(ctx, req)
	require.NoError(t, err)
	require.Equal(t, original.ID, tokens[f.issuerID].RemoteSessionID)
	require.Equal(t, original.Resource.String, tokens[f.issuerID].Resource, "stored spelling stays verbatim")
	states, err := f.mgr.AuthClientRemoteSessionStatusesForResource(ctx, subject, f.projectID, f.organizationID, []uuid.UUID{f.clientID}, req.Resource)
	require.NoError(t, err)
	require.Equal(t, original.ID, states[f.clientID].ID)
	// Distinct stored spellings that match the same routed audience must never
	// be silently collapsed into one token, including by the readiness reader.
	params.Resource = conv.ToPGText(req.Resource)
	duplicate, err := q.UpsertRemoteSession(ctx, params)
	require.NoError(t, err)
	require.NotEqual(t, original.ID, duplicate.ID)
	_, err = q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: f.clientID, Resource: req.Resource})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.mgr.ResolveAuthClientAccessTokens(ctx, req)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken)
	states, err = f.mgr.AuthClientRemoteSessionStatusesForResource(ctx, subject, f.projectID, f.organizationID, []uuid.UUID{f.clientID}, req.Resource)
	require.NoError(t, err)
	require.Empty(t, states)
}
