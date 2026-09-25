package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

type testAgentSessionRevoker struct {
	events           []string
	pushErr          error
	cascadeErr       error
	afterCommit      func()
	checkPushContext func(context.Context)
	credentials      int
	lastCredentials  []remotesessions.RevokedCredentials
	revokedJTIs      []string
}

func (r *testAgentSessionRevoker) RevokeToken(ctx context.Context, jti string) error {
	r.events = append(r.events, "token")
	r.revokedJTIs = append(r.revokedJTIs, jti)
	if r.afterCommit != nil {
		r.afterCommit()
	}
	if r.checkPushContext != nil {
		r.checkPushContext(ctx)
	}
	return r.pushErr
}
func (r *testAgentSessionRevoker) SoftDeleteAgentSubjectSessions(ctx context.Context, tx remoterepo.DBTX, subject urn.SessionSubject, issuerID, projectID uuid.UUID, orgID string, revokedAt pgtype.Timestamptz, alreadyRevoked bool) ([]remotesessions.RevokedCredentials, error) {
	r.events = append(r.events, "cascade")
	if r.cascadeErr != nil {
		return nil, r.cascadeErr
	}
	credentials, err := (&remotesessions.UpstreamRevoker{}).SoftDeleteAgentSubjectSessions(ctx, tx, subject, issuerID, projectID, orgID, revokedAt, alreadyRevoked)
	if err != nil {
		return nil, fmt.Errorf("delete test subject sessions: %w", err)
	}
	return credentials, nil
}
func (r *testAgentSessionRevoker) RevokeAllDetached(_ context.Context, creds []remotesessions.RevokedCredentials) {
	r.events = append(r.events, "upstream")
	r.credentials += len(creds)
	r.lastCredentials = creds
}

func seedManagedSession(t *testing.T, db *pgxpool.Pool, orgID, subject string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	issuer, err := usersessionsrepo.New(db).CreateOrganizationUserSessionIssuer(t.Context(), usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID:               conv.ToPGText(orgID),
		Slug:                         "issuer-" + uuid.NewString(),
		AuthnChallengeMode:           "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TrustedRemoteSessionClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	session := createManagedUserSession(t, db, issuer.ID, subject, "jti-"+uuid.NewString(), "hash-"+uuid.NewString())
	return session, issuer.ID
}

// createManagedUserSession mints an agent-bound session on an existing issuer.
// It carries no real credential material.
func createManagedUserSession(t *testing.T, db *pgxpool.Pool, issuerID uuid.UUID, subject, jti, refreshHash string) uuid.UUID {
	t.Helper()
	sessionSubject, err := urn.ParseSessionSubject(subject)
	require.NoError(t, err)
	now := time.Now()
	session, err := usersessionsrepo.New(db).CreateUserSession(t.Context(), usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: issuerID,
		SubjectUrn:          sessionSubject,
		AuthorizerUserID:    conv.ToPGText("owner"),
		Jti:                 jti,
		RefreshTokenHash:    conv.ToPGText(refreshHash),
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: 0, Valid: true},
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), InfinityModifier: 0, Valid: true},
	})
	require.NoError(t, err)
	return session.ID
}

// liveUserSession reads a session through the tenancy-scoped production query,
// which only returns live rows.
func liveUserSession(ctx context.Context, db *pgxpool.Pool, orgID string, sessionID uuid.UUID) (usersessionsrepo.UserSession, error) {
	session, err := usersessionsrepo.New(db).GetUserSessionByID(ctx, usersessionsrepo.GetUserSessionByIDParams{ID: sessionID, ProjectID: uuid.Nil, OrganizationID: orgID})
	if err != nil {
		return usersessionsrepo.UserSession{}, fmt.Errorf("get user session: %w", err)
	}
	return session, nil
}

func seedManagedUpstream(t *testing.T, db *pgxpool.Pool, orgID string, issuerID uuid.UUID, subject string) uuid.UUID {
	t.Helper()
	return seedManagedUpstreamWith(t, db, orgID, issuerID, subject, nil).ID
}

// seedManagedUpstreamWith seeds a minimal upstream cascade fixture. The
// ciphertext is never decrypted by the cascade tests.
func seedManagedUpstreamWith(t *testing.T, db *pgxpool.Pool, orgID string, issuerID uuid.UUID, subject string, customize func(*remoterepo.UpsertRemoteSessionParams)) remoterepo.RemoteSession {
	t.Helper()
	q := remoterepo.New(db)
	slug := "remote-" + uuid.NewString()
	remoteIssuer, err := q.CreateRemoteSessionIssuer(t.Context(), remoterepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:                    conv.ToPGText(orgID),
		Slug:                              slug,
		Issuer:                            "https://idp.example.test",
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{},
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: []string{},
	})
	require.NoError(t, err)
	client, err := q.CreateRemoteSessionClient(t.Context(), remoterepo.CreateRemoteSessionClientParams{
		ProjectID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:        conv.ToPGText(orgID),
		RemoteSessionIssuerID: remoteIssuer.ID,
		ClientID:              "fixture",
	})
	require.NoError(t, err)
	sessionSubject, err := urn.ParseSessionSubject(subject)
	require.NoError(t, err)
	params := remoterepo.UpsertRemoteSessionParams{
		SubjectUrn:            sessionSubject,
		UserSessionIssuerID:   issuerID,
		RemoteSessionClientID: client.ID,
		AccessTokenEncrypted:  "fixture-not-a-token",
		RefreshTokenEncrypted: conv.ToPGText("fixture-refresh"),
		Scopes:                []string{},
	}
	if customize != nil {
		customize(&params)
	}
	session, err := q.UpsertRemoteSession(t.Context(), params)
	require.NoError(t, err)
	return session
}

func TestAgentSessionsScopeAndRevocationCascade(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	for _, org := range []string{"org-a", "org-b"} {
		seedOrganization(t, db, org)
		seedOrganizationUser(t, db, org, "owner")
	}
	seedOrganizationUser(t, db, "org-a", "other")
	agent := createAgent(t, db, "org-a", "owner", "Session agent")
	other := createAgent(t, db, "org-a", "owner", "Other agent")
	subject := "agent:" + agent.ID.String()
	session, issuer := seedManagedSession(t, db, "org-a", subject)
	second, _ := seedManagedSession(t, db, "org-a", subject)
	human, _ := seedManagedSession(t, db, "org-a", "user:owner")
	otherSession, _ := seedManagedSession(t, db, "org-a", "agent:"+other.ID.String())
	crossTenant, _ := seedManagedSession(t, db, "org-b", subject)
	upstream := seedManagedUpstream(t, db, "org-a", issuer, subject)
	humanUpstream := seedManagedUpstream(t, db, "org-a", issuer, "user:owner")
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	listed, err := service.ListSessions(ctx, &gen.ListSessionsPayload{AgentID: agent.ID.String(), Limit: 1})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.NotNil(t, listed.NextCursor)
	next, err := service.ListSessions(ctx, &gen.ListSessionsPayload{AgentID: agent.ID.String(), Limit: 1, Cursor: listed.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.Nil(t, next.NextCursor)
	require.ElementsMatch(t, []string{session.String(), second.String()}, []string{listed.Items[0].ID, next.Items[0].ID})
	revoker := &testAgentSessionRevoker{}
	service.sessionTokens, service.sessionRevoker = revoker, revoker
	for _, id := range []uuid.UUID{human, otherSession, crossTenant, uuid.New()} {
		err := service.RevokeSession(ctx, &gen.RevokeSessionPayload{AgentID: agent.ID.String(), SessionID: id.String()})
		requireOopsCode(t, err, oops.CodeNotFound)
	}
	require.Empty(t, revoker.events)
	for _, denied := range []context.Context{validatedHumanContext(t, "org-a", "other"), validatedHumanContext(t, "org-a", "absent"), contextvalues.WithValidatedGramSession(ctx, mustAuthContext(t, ctx), true)} {
		_, err := service.ListSessions(denied, &gen.ListSessionsPayload{AgentID: agent.ID.String()})
		requireOopsCode(t, err, oops.CodeForbidden)
		requireOopsCode(t, service.RevokeSession(denied, &gen.RevokeSessionPayload{AgentID: agent.ID.String(), SessionID: session.String()}), oops.CodeForbidden)
	}
	revoker.afterCommit = func() {
		_, err := liveUserSession(ctx, db, "org-a", session)
		require.ErrorIs(t, err, pgx.ErrNoRows, "revocation must commit before token invalidation")
	}
	require.NoError(t, service.RevokeSession(ctx, &gen.RevokeSessionPayload{AgentID: agent.ID.String(), SessionID: session.String()}))
	require.Equal(t, []string{"cascade", "token", "upstream"}, revoker.events)
	require.Equal(t, 1, revoker.credentials)
	var revoked, preserved bool
	require.NoError(t, db.QueryRow(ctx, `SELECT deleted FROM remote_sessions WHERE id=$1`, upstream).Scan(&revoked))        //nolint:glint // notestingrawsql: validates real upstream cascade SQL
	require.NoError(t, db.QueryRow(ctx, `SELECT deleted FROM remote_sessions WHERE id=$1`, humanUpstream).Scan(&preserved)) //nolint:glint // notestingrawsql: proves no owner credential cascade
	require.True(t, revoked)
	require.False(t, preserved)
	listed, err = service.ListSessions(ctx, &gen.ListSessionsPayload{AgentID: agent.ID.String()})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
}

func TestAgentSessionRevocationFailureOrderingAndRetry(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-a")
	seedOrganizationUser(t, db, "org-a", "owner")
	agent := createAgent(t, db, "org-a", "owner", "Agent")
	session, issuer := seedManagedSession(t, db, "org-a", "agent:"+agent.ID.String())
	seededSession, err := liveUserSession(t.Context(), db, "org-a", session)
	require.NoError(t, err)
	// Identity fields exercise identity cleanup on retryable tombstones.
	seededUpstream := seedManagedUpstreamWith(t, db, "org-a", issuer, "agent:"+agent.ID.String(), func(params *remoterepo.UpsertRemoteSessionParams) {
		params.UpstreamSubject = conv.ToPGText("fixture-subject")
		params.UpstreamEmail = conv.ToPGText("fixture@example.test")
		params.UpstreamDisplayName = conv.ToPGText("Fixture")
		params.IdentitySource = conv.ToPGText("id_token")
		params.Enrichment = []byte(`{"team":"fixture"}`)
	})
	upstream := seededUpstream.ID
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	revoker := &testAgentSessionRevoker{cascadeErr: errors.New("cascade unavailable")}
	service.sessionTokens, service.sessionRevoker = revoker, revoker
	payload := &gen.RevokeSessionPayload{AgentID: agent.ID.String(), SessionID: session.String()}
	require.Error(t, service.RevokeSession(ctx, payload))
	require.Equal(t, []string{"cascade"}, revoker.events)
	_, err = liveUserSession(ctx, db, "org-a", session)
	require.NoError(t, err, "rollback must preserve refresh credentials")
	revoker.cascadeErr = nil
	revoker.pushErr = errors.New("cache unavailable")
	revoker.events = nil
	requireOopsCode(t, service.RevokeSession(ctx, payload), oops.CodeUnexpected)
	require.Equal(t, []string{"cascade", "token", "upstream"}, revoker.events)
	// Reconnect on the same issuer and remote client after the committed revoke.
	// The reconnect must retain its distinct session and tokens.
	freshSession := createManagedUserSession(t, db, issuer, "agent:"+agent.ID.String(), "fresh-jti", "fresh-hash")
	// Same subject and client, but fresh live credentials. The original row is
	// a committed tombstone, so this inserts a new live row beside it.
	fresh, err := remoterepo.New(db).UpsertRemoteSession(ctx, remoterepo.UpsertRemoteSessionParams{
		SubjectUrn:            seededUpstream.SubjectUrn,
		UserSessionIssuerID:   seededUpstream.UserSessionIssuerID,
		RemoteSessionClientID: seededUpstream.RemoteSessionClientID,
		AccessTokenEncrypted:  "fresh-access",
		RefreshTokenEncrypted: conv.ToPGText("fresh-refresh"),
		Scopes:                []string{},
		UpstreamSubject:       conv.ToPGText("fresh-identity"),
	})
	require.NoError(t, err)
	require.NotEqual(t, upstream, fresh.ID)
	freshUpstream := fresh.ID
	_, err = db.Exec(ctx, `INSERT INTO remote_sessions (subject_urn, user_session_issuer_id, remote_session_client_id, access_token_encrypted, deleted_at) SELECT subject_urn, user_session_issuer_id, remote_session_client_id, 'unrelated-old-access', deleted_at - interval '1 second' FROM remote_sessions WHERE id=$1`, upstream) //nolint:glint // notestingrawsql: an older unrelated tombstone must not match the retry boundary
	require.NoError(t, err)
	var sameBoundary bool
	require.NoError(t, db.QueryRow(ctx, `SELECT r.deleted_at = s.deleted_at FROM remote_sessions r JOIN user_sessions s ON s.id=$2 WHERE r.id=$1`, upstream, session).Scan(&sameBoundary)) //nolint:glint // notestingrawsql: exact persisted boundary shared by original tombstones
	require.True(t, sameBoundary)
	revoker.pushErr = nil
	revoker.events = nil
	require.NoError(t, service.RevokeSession(ctx, payload))
	require.Len(t, revoker.lastCredentials, 1)
	require.Equal(t, "fixture-not-a-token", revoker.lastCredentials[0].AccessTokenEncrypted)
	require.Equal(t, "fixture-refresh", revoker.lastCredentials[0].RefreshTokenEncrypted.String)
	require.Equal(t, []string{seededSession.Jti, seededSession.Jti}, revoker.revokedJTIs)
	var freshPreserved bool
	require.NoError(t, db.QueryRow(ctx, `SELECT NOT deleted AND access_token_encrypted='fresh-access' AND refresh_token_encrypted='fresh-refresh' AND upstream_subject='fresh-identity' FROM remote_sessions WHERE id=$1`, freshUpstream).Scan(&freshPreserved)) //nolint:glint // notestingrawsql: retry cannot alter the reconnect's credentials or identity
	require.True(t, freshPreserved)
	// The reconnect's JWT and refresh session remain live.
	freshStored, err := liveUserSession(ctx, db, "org-a", freshSession)
	require.NoError(t, err)
	require.Equal(t, "fresh-jti", freshStored.Jti)
	require.Equal(t, conv.ToPGText("fresh-hash"), freshStored.RefreshTokenHash)
	require.Equal(t, []string{"cascade", "token", "upstream"}, revoker.events)
	require.Equal(t, 2, revoker.credentials, "retry must repeat upstream revocation with retained credentials")
	require.Len(t, auditLogs(t, db, "org-a", session.String()), 1, "audit idempotency across a committed retry")
	var identityCleared bool
	require.NoError(t, db.QueryRow(ctx, `SELECT upstream_subject IS NULL AND upstream_email IS NULL AND upstream_display_name IS NULL AND identity_source IS NULL AND enrichment IS NULL FROM remote_sessions WHERE id=$1 AND deleted`, upstream).Scan(&identityCleared)) //nolint:glint // notestingrawsql: retries retain credentials but must remove identity
	require.True(t, identityCleared)
	var subject, access, refresh string
	require.NoError(t, db.QueryRow(ctx, `SELECT subject_urn, access_token_encrypted, refresh_token_encrypted FROM remote_sessions WHERE id=$1 AND deleted`, upstream).Scan(&subject, &access, &refresh)) //nolint:glint // notestingrawsql: runtime tombstones retain linkage and encrypted credentials for retries
	require.Equal(t, "agent:"+agent.ID.String(), subject)
	require.Equal(t, "fixture-not-a-token", access)
	require.Equal(t, "fixture-refresh", refresh)
}

func TestAgentSessionRevocationDetachedCachePush(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	seedOrganization(t, db, "org-a")
	seedOrganizationUser(t, db, "org-a", "owner")
	agent := createAgent(t, db, "org-a", "owner", "Agent")
	session, _ := seedManagedSession(t, db, "org-a", "agent:"+agent.ID.String())
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx, cancel := context.WithCancel(validatedHumanContext(t, "org-a", "owner"))
	defer cancel()
	revoker := &testAgentSessionRevoker{afterCommit: cancel}
	revoker.checkPushContext = func(pushCtx context.Context) {
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.NoError(t, pushCtx.Err())
		deadline, ok := pushCtx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(5*time.Second), deadline, time.Second)
	}
	service.sessionTokens, service.sessionRevoker = revoker, revoker
	require.NoError(t, service.RevokeSession(ctx, &gen.RevokeSessionPayload{AgentID: agent.ID.String(), SessionID: session.String()}))
	require.Equal(t, []string{"cascade", "token", "upstream"}, revoker.events)
}
