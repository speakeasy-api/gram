package agentmanagement

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestAgentSessionLegacyOrganizationFallback(t *testing.T) {
	t.Parallel()
	orgA, orgB, unset := conv.ToPGText("org-a"), conv.ToPGText("org-b"), pgtype.Text{String: "", Valid: false}
	for _, tc := range []struct {
		name                          string
		issuerOrg, sessionOrg         pgtype.Text
		projectOrg, sessionProjectOrg string
		visible                       bool
	}{
		{"all sources agree", orgA, orgA, "org-a", "org-a", true},
		{"issuer fallback", orgA, unset, "org-a", "org-a", true},
		{"project fallback", unset, unset, "org-a", "org-a", true},
		{"session project fallback", unset, unset, "", "org-a", true},
		{"explicit session mismatch", orgA, orgB, "org-a", "org-a", false},
		{"explicit issuer mismatch", orgB, unset, "org-a", "org-a", false},
		{"session hides issuer mismatch", orgB, orgA, "org-a", "org-a", false},
		{"session hides project mismatch", unset, orgA, "org-b", "org-a", false},
		{"issuer hides project mismatch", orgA, unset, "org-b", "org-a", false},
		{"session and issuer hide project mismatch", orgA, orgA, "org-b", "org-a", false},
		{"session project mismatch", orgA, orgA, "org-a", "org-b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := newTestDB(t)
			seedOrganization(t, db, "org-a")
			seedOrganization(t, db, "org-b")
			seedOrganizationUser(t, db, "org-a", "owner")
			agent := createAgent(t, db, "org-a", "owner", "Legacy agent")
			session, issuer := seedManagedSession(t, db, "org-a", "agent:"+agent.ID.String())
			project := uuid.New()
			projectOrg := tc.projectOrg
			if projectOrg == "" {
				projectOrg = "org-a"
			}
			_, err := testrepo.New(db).CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{ID: project, Name: "Legacy project", Slug: "legacy", OrganizationID: projectOrg})
			require.NoError(t, err)
			err = testrepo.New(db).SetUserSessionIssuerTenancyFixture(t.Context(), testrepo.SetUserSessionIssuerTenancyFixtureParams{
				ProjectID: uuid.NullUUID{UUID: project, Valid: tc.projectOrg != ""}, OrganizationID: tc.issuerOrg, ID: issuer,
			})
			require.NoError(t, err)
			sessionProject := uuid.New()
			_, err = testrepo.New(db).CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{ID: sessionProject, Name: "Session project", Slug: "session", OrganizationID: tc.sessionProjectOrg})
			require.NoError(t, err)
			err = testrepo.New(db).SetUserSessionTenancyFixture(t.Context(), testrepo.SetUserSessionTenancyFixtureParams{
				ProjectID: uuid.NullUUID{UUID: sessionProject, Valid: true}, OrganizationID: tc.sessionOrg, ID: session,
			})
			require.NoError(t, err)
			var upstream uuid.UUID
			if tc.projectOrg != "" {
				upstream = seedManagedUpstreamWith(t, db, "org-a", issuer, "agent:"+agent.ID.String(), nil).ID
				// The cascade must use the issuer project, not the different same-org session project.
				err = testrepo.New(db).SetRemoteSessionClientProjectFixture(t.Context(), testrepo.SetRemoteSessionClientProjectFixtureParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, RemoteSessionID: upstream})
				require.NoError(t, err)
			}
			service := newTestService(db, &fakeAuthorizationEngine{allowed: map[string]bool{}})
			revoker := &testAgentSessionRevoker{}
			service.sessionTokens, service.sessionRevoker = revoker, revoker
			ctx := validatedHumanContext(t, "org-a", "owner")
			listed, err := service.ListSessions(ctx, &gen.ListSessionsPayload{AgentID: agent.ID.String()})
			require.NoError(t, err)
			err = service.RevokeSession(ctx, &gen.RevokeSessionPayload{AgentID: agent.ID.String(), SessionID: session.String()})
			if tc.visible {
				require.Len(t, listed.Items, 1)
				require.NoError(t, err)
				if upstream != uuid.Nil {
					require.Equal(t, 1, revoker.credentials)
					stored, err := remoterepo.New(db).GetRemoteSessionByIDIncludingDeleted(ctx, remoterepo.GetRemoteSessionByIDIncludingDeletedParams{ID: upstream, ProjectID: uuid.NullUUID{UUID: project, Valid: true}})
					require.NoError(t, err)
					require.True(t, stored.Deleted, "mismatched same-org projects still cascade upstream")
				}
			} else {
				require.Empty(t, listed.Items)
				requireOopsCode(t, err, oops.CodeNotFound)
				require.Empty(t, revoker.events)
				deleted, err := testrepo.New(db).GetUserSessionDeletedFixture(ctx, session)
				require.NoError(t, err)
				require.False(t, deleted, "denied revocation must not mutate the session")
			}
		})
	}
}
