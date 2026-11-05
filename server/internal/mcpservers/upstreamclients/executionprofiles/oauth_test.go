package executionprofiles

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	binding, err := f.q.GetOAuthIssuerBindingFixture(t.Context(), GetOAuthIssuerBindingFixtureParams{
		AuthClientID: selected, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, IssuerID: seeded.IssuerID,
	})
	require.NoError(t, err)
	require.Equal(t, selectedRegistration, binding.RemoteSessionClientID)
	require.Equal(t, seeded.IssuerID, binding.UserSessionIssuerID)
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
			project := uuid.NullUUID{UUID: f.projectID, Valid: true}
			if scope != "project" {
				require.NoError(t, f.q.SetOAuthIssuerScopeFixture(t.Context(), SetOAuthIssuerScopeFixtureParams{
					IssuerID: in.IssuerID, ProjectID: project, GlobalScope: scope == "global",
				}))
			}
			signer := sessiontokens.NewSigner("oauth-fixture-secret")
			raw, jti, err := signer.Mint(sessiontokens.MintParams{Subject: session.Subject(), Audience: "oauth-fixture", Issuer: session.Issuer(), ClientID: session.ClientID(), Lifetime: time.Hour})
			require.NoError(t, err)
			refreshed, err := signer.ValidateBearer(t.Context(), raw, "oauth-fixture", oauthNotRevoked{})
			require.NoError(t, err)
			// Rotation retires the predecessor and inserts a new row and JTI,
			// retaining the stable credential referenced by the original selection.
			rotation, err := f.q.RefreshOAuthSessionFixture(t.Context(), RefreshOAuthSessionFixtureParams{
				OldJti: session.JTI(), NewJti: jti, ProjectID: project,
			})
			require.NoError(t, err)
			require.NotEqual(t, rotation.OldID, rotation.NewID)
			require.Equal(t, session.JTI(), rotation.OldJti)
			require.Equal(t, refreshed.JTI(), rotation.NewJti)
			require.NotEqual(t, rotation.OldJti, rotation.NewJti)
			require.True(t, rotation.OldDeleted)
			require.Equal(t, uuid.NullUUID{UUID: rotation.OldID, Valid: true}, rotation.CredentialID)
			_, err = AdoptOAuthSelection(t.Context(), f.pool, in, session)
			require.Error(t, err, "retired predecessor cannot publish a choice")
			_, err = AdoptOAuthSelection(t.Context(), f.pool, in, refreshed)
			require.NoError(t, err)
			require.Equal(t, map[uuid.UUID]uuid.UUID{in.ServerID: selected}, f.choices(f.bind(ContextUserBacked).VersionID))
		})
	}
}

func TestAdoptOAuthSelectionRejectsWithdrawnChoice(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"disabled", "detached", "unbound issuer", "deleted registration"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, in, session, selected := oauthFixture(t, ContextUserBacked)
			err := f.q.WithdrawOAuthChoiceFixture(t.Context(), WithdrawOAuthChoiceFixtureParams{AuthClientID: selected, IssuerID: in.IssuerID, McpServerID: in.ServerID, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, Change: change})
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
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	_, err := New(tx).GetOAuthResumeSelection(t.Context(), GetOAuthResumeSelectionParams{
		Jti: session.JTI(), SubjectUrn: session.Subject(), ClientID: in.ClientID,
		IssuerID: in.IssuerID, McpServerID: in.ServerID, ProjectID: f.projectID, OrganizationID: f.orgID,
	})
	require.NoError(t, err)
	for _, change := range []string{"disabled", "detached", "unbound issuer", "deleted registration"} {
		other := testenv.BeginTx(t, t.Context(), f.pool)
		testenv.SetLockTimeout(t, t.Context(), other, 50*time.Millisecond)
		err = New(other).WithdrawOAuthChoiceFixture(t.Context(), WithdrawOAuthChoiceFixtureParams{AuthClientID: selected, IssuerID: in.IssuerID, McpServerID: in.ServerID, ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, Change: change})
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "55P03", pgErr.Code, "live evidence must stay locked until publication commits")
		require.NoError(t, other.Rollback(t.Context()))
	}
}
