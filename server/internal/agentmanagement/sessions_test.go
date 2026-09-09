package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type testAgentSessionRevoker struct {
	events      []string
	pushErr     error
	cascadeErr  error
	afterCommit func()
	credentials int
}

func (r *testAgentSessionRevoker) RevokeToken(_ context.Context, _ string) error {
	r.events = append(r.events, "token")
	if r.afterCommit != nil {
		r.afterCommit()
	}
	return r.pushErr
}
func (r *testAgentSessionRevoker) SoftDeleteSubjectSessions(ctx context.Context, tx remoterepo.DBTX, subject urn.SessionSubject, issuerID, projectID uuid.UUID, orgID string) ([]remotesessions.RevokedCredentials, error) {
	r.events = append(r.events, "cascade")
	if r.cascadeErr != nil {
		return nil, r.cascadeErr
	}
	credentials, err := (&remotesessions.UpstreamRevoker{}).SoftDeleteSubjectSessions(ctx, tx, subject, issuerID, projectID, orgID)
	if err != nil {
		return nil, fmt.Errorf("delete test subject sessions: %w", err)
	}
	return credentials, nil
}
func (r *testAgentSessionRevoker) RevokeAllDetached(_ context.Context, creds []remotesessions.RevokedCredentials) {
	r.events = append(r.events, "upstream")
	r.credentials += len(creds)
}

func seedManagedSession(t *testing.T, db *pgxpool.Pool, orgID, subject string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	issuerID, sessionID := uuid.New(), uuid.New()
	_, err := db.Exec(t.Context(), `INSERT INTO user_session_issuers (id, organization_id, slug, authn_challenge_mode, session_duration) VALUES ($1,$2,$3,'interactive','1 hour')`, issuerID, orgID, "issuer-"+issuerID.String()) //nolint:glint // notestingrawsql: agent-bound issuer fixture
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO user_sessions (id, organization_id, user_session_issuer_id, subject_urn, authorizer_user_id, jti, refresh_token_hash, expires_at, refresh_expires_at) VALUES ($1,$2,$3,$4,'owner',$5,$6,clock_timestamp()+'1 hour',clock_timestamp()+'1 day')`, sessionID, orgID, issuerID, subject, "jti-"+sessionID.String(), "hash-"+sessionID.String()) //nolint:glint // notestingrawsql: agent-bound session fixture, no real credential material
	require.NoError(t, err)
	return sessionID, issuerID
}

func seedManagedUpstream(t *testing.T, db *pgxpool.Pool, orgID string, issuerID uuid.UUID, subject string) uuid.UUID {
	t.Helper()
	remoteIssuer, client, session := uuid.New(), uuid.New(), uuid.New()
	_, err := db.Exec(t.Context(), `INSERT INTO remote_session_issuers (id, organization_id, slug, issuer) VALUES ($1,$2,$3,'https://idp.example.test')`, remoteIssuer, orgID, "remote-"+remoteIssuer.String()) //nolint:glint // notestingrawsql: minimal upstream cascade fixture
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO remote_session_clients (id, organization_id, remote_session_issuer_id, client_id) VALUES ($1,$2,$3,'fixture')`, client, orgID, remoteIssuer) //nolint:glint // notestingrawsql: minimal upstream cascade fixture
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO remote_sessions (id, subject_urn, user_session_issuer_id, remote_session_client_id, access_token_encrypted) VALUES ($1,$2,$3,$4,'fixture-not-a-token')`, session, subject, issuerID, client) //nolint:glint // notestingrawsql: ciphertext is never decrypted by this cascade test
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
		var deleted bool
		require.NoError(t, db.QueryRow(ctx, `SELECT deleted FROM user_sessions WHERE id=$1`, session).Scan(&deleted)) //nolint:glint // notestingrawsql: confirms commit precedes token invalidation
		require.True(t, deleted)
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
	session, _ := seedManagedSession(t, db, "org-a", "agent:"+agent.ID.String())
	service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
	ctx := validatedHumanContext(t, "org-a", "owner")
	revoker := &testAgentSessionRevoker{cascadeErr: errors.New("cascade unavailable")}
	service.sessionTokens, service.sessionRevoker = revoker, revoker
	payload := &gen.RevokeSessionPayload{AgentID: agent.ID.String(), SessionID: session.String()}
	require.Error(t, service.RevokeSession(ctx, payload))
	require.Equal(t, []string{"cascade"}, revoker.events)
	var deleted bool
	require.NoError(t, db.QueryRow(ctx, `SELECT deleted FROM user_sessions WHERE id=$1`, session).Scan(&deleted)) //nolint:glint // notestingrawsql: rollback must preserve refresh credentials
	require.False(t, deleted)
	revoker.cascadeErr = nil
	revoker.pushErr = errors.New("cache unavailable")
	revoker.events = nil
	requireOopsCode(t, service.RevokeSession(ctx, payload), oops.CodeUnexpected)
	require.Equal(t, []string{"cascade", "token", "upstream"}, revoker.events)
	revoker.pushErr = nil
	revoker.events = nil
	require.NoError(t, service.RevokeSession(ctx, payload))
	require.Equal(t, []string{"cascade", "token", "upstream"}, revoker.events)
}
