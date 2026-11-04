package executionprofiles

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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
	selected := f.authClient(server, typeRemoteSession, f.registration(server))
	old := f.bind(execCtx)
	require.Len(t, old.Gaps, 1)
	signer := sessiontokens.NewSigner("oauth-fixture-secret")
	raw, jti, err := signer.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject("oauth-user"), Audience: "oauth-fixture", Issuer: "https://auth.example.com", ClientID: "oauth-fixture", Lifetime: time.Hour})
	require.NoError(t, err)
	session, err := signer.ValidateBearer(t.Context(), raw, "oauth-fixture", oauthNotRevoked{})
	require.NoError(t, err)
	seeded, err := f.q.SeedOAuthResumeFixture(t.Context(), SeedOAuthResumeFixtureParams{ProjectID: f.projectID, OrganizationID: f.orgID, AssistantID: f.assistant, McpServerID: server, AuthClientID: selected, Jti: jti})
	require.NoError(t, err)
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
