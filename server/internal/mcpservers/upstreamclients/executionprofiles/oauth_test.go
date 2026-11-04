package executionprofiles

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

type oauthNotRevoked struct{}

func (oauthNotRevoked) IsTokenRevoked(context.Context, string) (bool, error) { return false, nil }

func oauthFixture(t *testing.T, execCtx Context) (*fixture, OAuthResume, sessiontokens.ValidatedSession, uuid.UUID) {
	t.Helper()
	f := newFixture(t)
	server := f.server(pgtype.Bool{Bool: true, Valid: true})
	f.authClient(server, typeRemoteSession, f.registration(server))
	selectedRegistration := f.registration(server)
	selected := f.authClient(server, typeRemoteSession, selectedRegistration)
	old := f.bind(execCtx)
	require.Len(t, old.Gaps, 1)
	signer := sessiontokens.NewSigner("oauth-fixture-secret")
	raw, jti, err := signer.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject("oauth-user"), Audience: "oauth-fixture", Issuer: "https://auth.example.com", ClientID: "oauth-fixture", Lifetime: time.Hour})
	require.NoError(t, err)
	session, err := signer.ValidateBearer(t.Context(), raw, "oauth-fixture", oauthNotRevoked{})
	require.NoError(t, err)
	seeded, err := f.q.SeedOAuthResumeFixture(t.Context(), SeedOAuthResumeFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, AssistantID: f.assistant, McpServerID: server, AuthClientID: selected, Jti: jti})
	require.NoError(t, err)
	var registration, issuer uuid.UUID
	err = f.pool.QueryRow(t.Context(), `SELECT binding.remote_session_client_id, binding.user_session_issuer_id
		FROM remote_session_client_user_session_issuers binding
		JOIN mcp_auth_clients ac ON ac.remote_session_client_id = binding.remote_session_client_id
		WHERE ac.id = $1 AND ac.project_id = $2 AND binding.user_session_issuer_id = $3`, selected, f.projectID, seeded.IssuerID).Scan(&registration, &issuer)
	require.NoError(t, err)
	require.Equal(t, selectedRegistration, registration)
	require.Equal(t, seeded.IssuerID, issuer)
	in := OAuthResume{Origin: GetOAuthResumeOriginParams{VersionID: old.VersionID, OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: f.assistant, ThreadID: seeded.ThreadID}, UserID: "oauth-user", ClientID: "oauth-fixture", IssuerID: seeded.IssuerID, ServerID: server}
	return f, in, session, selected
}

func TestAdoptOAuthSelectionResumesWithExactChoice(t *testing.T) {
	t.Parallel()
	f, in, session, selected := oauthFixture(t, ContextUserBacked)
	trigger, err := AdoptOAuthSelection(t.Context(), f.pool, in, session)
	require.NoError(t, err)
	require.Equal(t, f.trigger, trigger)
	next := f.bind(ContextUserBacked)
	require.Empty(t, next.Gaps)
	require.Equal(t, map[uuid.UUID]uuid.UUID{in.ServerID: selected}, f.choices(next.VersionID))
	require.Empty(t, f.choices(in.Origin.VersionID), "already-issued turns must remain immutable")
	_, err = AdoptOAuthSelection(t.Context(), f.pool, in, session)
	require.NoError(t, err, "same grant is idempotent")
	require.Equal(t, next.VersionID, f.bind(ContextUserBacked).VersionID)
	userless := f.bind(ContextUserless)
	require.Empty(t, f.choices(userless.VersionID), "browser authentication must not expand scheduled authority")
}

func TestAdoptOAuthSelectionRejectsWrongOrigin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		mutate func(*OAuthResume)
	}{
		{"wrong user", func(in *OAuthResume) { in.UserID = "other-user" }},
		{"userless", func(in *OAuthResume) { in.UserID = "" }},
		{"wrong tenant", func(in *OAuthResume) { in.Origin.OrganizationID = "other-org" }},
		{"wrong project", func(in *OAuthResume) { in.Origin.ProjectID = uuid.New() }},
		{"wrong assistant", func(in *OAuthResume) { in.Origin.AssistantID = uuid.New() }},
		{"wrong thread", func(in *OAuthResume) { in.Origin.ThreadID = uuid.New() }},
		{"wrong client", func(in *OAuthResume) { in.ClientID = "other-client" }},
		{"wrong issuer", func(in *OAuthResume) { in.IssuerID = uuid.New() }},
		{"wrong server", func(in *OAuthResume) { in.ServerID = uuid.New() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, in, session, _ := oauthFixture(t, ContextUserBacked)
			original := in.Origin.VersionID
			tc.mutate(&in)
			_, err := AdoptOAuthSelection(t.Context(), f.pool, in, session)
			require.Error(t, err)
			require.Empty(t, f.choices(original))
			require.Equal(t, original, f.bind(ContextUserBacked).VersionID)
		})
	}
}

func TestAdoptOAuthSelectionRejectsUserlessProfile(t *testing.T) {
	t.Parallel()
	f, in, session, _ := oauthFixture(t, ContextUserless)
	_, err := AdoptOAuthSelection(t.Context(), f.pool, in, session)
	require.Error(t, err)
	require.Empty(t, f.choices(in.Origin.VersionID))
}

func TestAdoptOAuthSelectionDoesNotOverwriteNewerChoice(t *testing.T) {
	t.Parallel()
	f, in, session, _ := oauthFixture(t, ContextUserBacked)
	other := f.authClient(in.ServerID, typeRemoteSession, f.registration(in.ServerID))
	_, err := f.setChoice(ContextUserBacked, in.ServerID, client(other))
	require.NoError(t, err)
	_, err = AdoptOAuthSelection(t.Context(), f.pool, in, session)
	require.ErrorIs(t, err, ErrVersionChanged)
	require.Equal(t, map[uuid.UUID]uuid.UUID{in.ServerID: other}, f.choices(f.bind(ContextUserBacked).VersionID))
}

func TestAdoptOAuthSelectionIssuerScopesAndRefresh(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"project", "organization", "global"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			f, in, session, selected := oauthFixture(t, ContextUserBacked)
			if scope != "project" {
				_, err := f.pool.Exec(t.Context(), `UPDATE user_session_issuers SET project_id = NULL,
					organization_id = CASE WHEN $3 THEN NULL ELSE organization_id END
					WHERE id = $1 AND project_id = $2`, in.IssuerID, f.projectID, scope == "global")
				require.NoError(t, err)
			}
			// Refresh creates a new row identity but preserves the selected credential.
			_, err := f.pool.Exec(t.Context(), `UPDATE user_sessions SET credential_id = id, id = $1
				WHERE jti = $2 AND project_id = $3`, uuid.New(), session.JTI(), f.projectID)
			require.NoError(t, err)
			_, err = AdoptOAuthSelection(t.Context(), f.pool, in, session)
			require.NoError(t, err)
			require.Equal(t, map[uuid.UUID]uuid.UUID{in.ServerID: selected}, f.choices(f.bind(ContextUserBacked).VersionID))
		})
	}
}

func TestAdoptOAuthSelectionRejectsWithdrawnChoice(t *testing.T) {
	t.Parallel()
	for _, change := range []struct {
		name string
		sql  string
	}{
		{"disabled", `UPDATE mcp_auth_clients SET enabled = FALSE WHERE id = $1 AND project_id = $2`},
		{"detached", `UPDATE mcp_server_auth_clients SET deleted_at = now() WHERE auth_client_id = $1 AND project_id = $2`},
		{"unbound issuer", `DELETE FROM remote_session_client_user_session_issuers WHERE remote_session_client_id = (SELECT remote_session_client_id FROM mcp_auth_clients WHERE id = $1 AND project_id = $2)`},
		{"deleted registration", `UPDATE remote_session_clients SET deleted_at = now() WHERE id = (SELECT remote_session_client_id FROM mcp_auth_clients WHERE id = $1 AND project_id = $2)`},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()
			f, in, session, selected := oauthFixture(t, ContextUserBacked)
			_, err := f.pool.Exec(t.Context(), change.sql, selected, f.projectID)
			require.NoError(t, err)
			_, err = AdoptOAuthSelection(t.Context(), f.pool, in, session)
			require.Error(t, err)
			profile, err := f.q.GetProfile(t.Context(), GetProfileParams{
				OrganizationID: f.orgID, ProjectID: f.projectID, AssistantID: f.assistant,
				TriggerInstanceID: f.trigger, ExecutionContext: string(ContextUserBacked),
			})
			require.NoError(t, err)
			require.Equal(t, in.Origin.VersionID, profile.VersionID)
			require.Empty(t, f.choices(in.Origin.VersionID))
		})
	}
}

func TestOAuthResumeSelectionLocksLiveChoice(t *testing.T) {
	t.Parallel()
	f, in, session, selected := oauthFixture(t, ContextUserBacked)
	tx, err := f.pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = New(tx).GetOAuthResumeSelection(t.Context(), GetOAuthResumeSelectionParams{
		Jti: session.JTI(), SubjectUrn: session.Subject(), ClientID: in.ClientID,
		IssuerID: in.IssuerID, McpServerID: in.ServerID, ProjectID: f.projectID, OrganizationID: f.orgID,
	})
	require.NoError(t, err)
	for _, query := range []string{
		`UPDATE mcp_auth_clients SET enabled = FALSE WHERE id = $1 AND project_id = $2`,
		`UPDATE mcp_server_auth_clients SET deleted_at = now() WHERE auth_client_id = $1 AND project_id = $2`,
		`DELETE FROM remote_session_client_user_session_issuers WHERE remote_session_client_id = (SELECT remote_session_client_id FROM mcp_auth_clients WHERE id = $1 AND project_id = $2)`,
		`UPDATE remote_session_clients SET deleted_at = now() WHERE id = (SELECT remote_session_client_id FROM mcp_auth_clients WHERE id = $1 AND project_id = $2)`,
	} {
		other, err := f.pool.Begin(t.Context())
		require.NoError(t, err)
		_, err = other.Exec(t.Context(), `SET LOCAL lock_timeout = '50ms'`)
		require.NoError(t, err)
		_, err = other.Exec(t.Context(), query, selected, f.projectID)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "55P03", pgErr.Code, "live evidence must stay locked until publication commits")
		require.NoError(t, other.Rollback(t.Context()))
	}
}
