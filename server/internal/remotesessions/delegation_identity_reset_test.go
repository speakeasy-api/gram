package remotesessions

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestDelegationResetKeepsInvalidatedDownstreamTokenClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                string
		registrationChanged bool
		refresh             string
		offlineRequested    bool
	}{
		{name: "new subject, no refresh token, offline not requested"},
		{name: "new subject, no refresh token, offline requested", offlineRequested: true},
		{name: "new subject, new refresh token", refresh: "other-refresh", offlineRequested: true},
		{name: "new registration, no refresh token, offline not requested", registrationChanged: true},
		{name: "new registration, no refresh token, offline requested", registrationChanged: true, offlineRequested: true},
		{name: "new registration, new refresh token", registrationChanged: true, refresh: "other-refresh", offlineRequested: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			s, store, p, b, allow := newDelegationUnitFixture(t)
			q := repo.New(s.db)
			subject := urn.NewUserSubject(b.HumanID).String()
			const resource = "https://resource.example.test/mcp"

			project, err := testrepo.New(s.db).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: "Reset project", Slug: "reset-project", OrganizationID: b.OrganizationID})
			require.NoError(t, err)
			usi, err := usersessionsrepo.New(s.db).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
				OrganizationID: pgtype.Text{String: b.OrganizationID, Valid: true}, Slug: "reset-usi", AuthnChallengeMode: "interactive",
				SessionDuration:              pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
				TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: b.IssuerID, Valid: true}, TrustedRemoteSessionClientID: uuid.NullUUID{UUID: b.ClientID, Valid: true},
			})
			require.NoError(t, err)
			key := repo.GetEMABindingParams{ProjectID: project, OrganizationID: b.OrganizationID, UserSessionIssuerID: usi.ID, RemoteSessionIssuerID: b.IssuerID, Resource: resource}
			require.NoError(t, q.EnsureEMABinding(ctx, repo.EnsureEMABindingParams(key)))
			current, err := q.GetEMABinding(ctx, key)
			require.NoError(t, err)
			binding, err := q.SetEMABinding(ctx, repo.SetEMABindingParams{
				State: pgtype.Text{String: PreparationStateReady, Valid: true}, RemoteSessionClientID: uuid.NullUUID{UUID: b.ClientID, Valid: true},
				Generation: current.Generation + 1, GrantSource: pgtype.Text{String: PreparationGrantSourceAdministratorDeclared, Valid: true},
				RequestedScopes: []string{}, ID: current.ID, ProjectID: project, OrganizationID: b.OrganizationID, ExpectedGeneration: current.Generation,
			})
			require.NoError(t, err)

			start := time.Now().UTC().Truncate(time.Microsecond).Add(-3 * time.Hour)
			moveTo := func(at time.Time) { s.now = func() time.Time { return at } }
			downstreamUsable := func() bool {
				t.Helper()
				row, err := q.GetEMACredentialForUse(ctx, repo.GetEMACredentialForUseParams{
					RemoteSessionIssuerID: b.IssuerID, BindingID: binding.ID, BindingGeneration: binding.Generation, RequestedScopes: []string{},
					UsableAfter: pgtype.Timestamptz{Time: time.Now(), Valid: true, InfinityModifier: pgtype.Finite}, TrustedClientID: b.ClientID,
					OrganizationID: b.OrganizationID, ProjectID: uuid.NullUUID{UUID: project, Valid: true}, UserSessionIssuerID: uuid.NullUUID{UUID: usi.ID, Valid: true},
					RemoteSessionClientID: uuid.NullUUID{UUID: b.ClientID, Valid: true}, Resource: resource, SubjectUrn: subject,
				})
				require.NoError(t, err)
				return row.Usable
			}

			moveTo(start)
			require.NoError(t, s.RetainVerifiedLogin(ctx, p, b.HumanID, delegationLogin(p, start, "first-id", "first-refresh", 30*time.Second), true))
			session, err := store.load(ctx, b)
			require.NoError(t, err)

			// A downstream token is minted at T1.
			_, err = q.PlantEMACredentialFixture(ctx, repo.PlantEMACredentialFixtureParams{
				OrganizationID: pgtype.Text{String: b.OrganizationID, Valid: true}, ProjectID: uuid.NullUUID{UUID: project, Valid: true},
				UserSessionIssuerID: uuid.NullUUID{UUID: usi.ID, Valid: true}, RemoteSessionIssuerID: uuid.NullUUID{UUID: b.IssuerID, Valid: true},
				RemoteSessionClientID: uuid.NullUUID{UUID: b.ClientID, Valid: true}, Resource: resource, SubjectUrn: subject, EmaProvider: string(EMAProviderExternal),
				TrustedIssuerSessionID: uuid.NullUUID{UUID: session.ID, Valid: true},
				UpdatedAt:              pgtype.Timestamptz{Time: start.Add(time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
			})
			require.NoError(t, err)
			require.True(t, downstreamUsable())

			// A later sign-in invalidates it, though it stays stored.
			signedIn := start.Add(2 * time.Hour)
			moveTo(signedIn)
			require.NoError(t, s.RetainVerifiedLogin(ctx, p, b.HumanID, delegationLogin(p, signedIn, "second-id", "second-refresh", 30*time.Second), true))
			require.False(t, downstreamUsable())

			resetAt := start.Add(3 * time.Hour)
			login := delegationLogin(p, resetAt, "other-id", tc.refresh, time.Hour)
			wantSubject := login.Subject
			if tc.registrationChanged {
				// An administrator rotates the client secret, then the person signs in.
				before := p.DelegationConfigurationHash()
				p.client.ClientSecretEncrypted = pgtype.Text{String: "rotated-secret", Valid: true}
				require.NotEqual(t, before, p.DelegationConfigurationHash())
			} else {
				// A refresh hits a configuration failure, then a different upstream
				// subject mapped to the same person signs in.
				s.refreshIdentity = func(context.Context, *FederatedProvider, string, string, string) (*FederatedRefreshResult, error) {
					return nil, &FederatedRefreshError{Kind: FederatedRefreshConfiguration}
				}
				_, err = s.Resolve(ctx, b, allow)
				require.ErrorIs(t, err, ErrDelegationConfiguration)
				require.False(t, downstreamUsable())
				login.Subject = "other-subject"
				wantSubject = "other-subject"
			}
			moveTo(resetAt)
			require.NoError(t, s.RetainVerifiedLogin(ctx, p, b.HumanID, login, tc.offlineRequested))
			row, err := store.load(ctx, b)
			require.NoError(t, err)
			require.Equal(t, wantSubject, delegationPlain(t, s, row.UpstreamSubjectEncrypted.String))
			require.False(t, downstreamUsable(), "a token invalidated before the reset must stay closed")
			require.WithinDuration(t, resetAt, row.CredentialObtainedAt.Time, time.Microsecond)

			assertion, err := s.Resolve(ctx, b, allow)
			require.NoError(t, err)
			require.Equal(t, "other-id", assertion.Value())
			require.WithinDuration(t, resetAt, assertion.ObtainedAt(), time.Microsecond)
		})
	}
}
