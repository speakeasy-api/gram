package agentmanagement

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestAgentSessionLegacyOrganizationFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                          string
		issuerOrg, sessionOrg         any
		projectOrg, sessionProjectOrg string
		visible                       bool
	}{
		{"all sources agree", "org-a", "org-a", "org-a", "org-a", true},
		{"issuer fallback", "org-a", nil, "org-a", "org-a", true},
		{"project fallback", nil, nil, "org-a", "org-a", true},
		{"session project fallback", nil, nil, "", "org-a", true},
		{"explicit session mismatch", "org-a", "org-b", "org-a", "org-a", false},
		{"explicit issuer mismatch", "org-b", nil, "org-a", "org-a", false},
		{"session hides issuer mismatch", "org-b", "org-a", "org-a", "org-a", false},
		{"session hides project mismatch", nil, "org-a", "org-b", "org-a", false},
		{"issuer hides project mismatch", "org-a", nil, "org-b", "org-a", false},
		{"session and issuer hide project mismatch", "org-a", "org-a", "org-b", "org-a", false},
		{"session project mismatch", "org-a", "org-a", "org-a", "org-b", false},
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
			_, err = db.Exec(t.Context(), `UPDATE user_session_issuers SET project_id=$1, organization_id=$2 WHERE id=$3`, uuid.NullUUID{UUID: project, Valid: tc.projectOrg != ""}, tc.issuerOrg, issuer) //nolint:glint // notestingrawsql: legacy nullable issuer tenancy
			require.NoError(t, err)
			sessionProject := uuid.New()
			_, err = testrepo.New(db).CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{ID: sessionProject, Name: "Session project", Slug: "session", OrganizationID: tc.sessionProjectOrg})
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `UPDATE user_sessions SET project_id=$1, organization_id=$2 WHERE id=$3`, sessionProject, tc.sessionOrg, session) //nolint:glint // notestingrawsql: legacy nullable session tenancy
			require.NoError(t, err)
			var upstream uuid.UUID
			if tc.projectOrg != "" {
				upstream = seedManagedUpstreamWith(t, db, "org-a", issuer, "agent:"+agent.ID.String(), nil).ID
				_, err = db.Exec(t.Context(), `UPDATE remote_session_clients SET project_id=$1 WHERE id=(SELECT remote_session_client_id FROM remote_sessions WHERE id=$2)`, project, upstream) //nolint:glint // notestingrawsql: cascade must use issuer project, not the different same-org session project
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
				var deleted bool
				require.NoError(t, db.QueryRow(ctx, `SELECT deleted FROM user_sessions WHERE id=$1`, session).Scan(&deleted)) //nolint:glint // notestingrawsql: denied revocation must not mutate the session
				require.False(t, deleted)
			}
		})
	}
}
