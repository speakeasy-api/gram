package agentmanagement

import (
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestAgentSessionLegacyOrganizationFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                  string
		issuerOrg, sessionOrg any
		visible               bool
	}{
		{"issuer fallback", "org-a", nil, true},
		{"project fallback", nil, nil, true},
		{"explicit session mismatch", "org-a", "org-b", false},
		{"explicit issuer mismatch", "org-b", nil, false},
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
			_, err := db.Exec(t.Context(), `INSERT INTO projects (id, organization_id, name, slug) VALUES ($1,'org-a','Legacy project','legacy')`, project) //nolint:glint // notestingrawsql: legacy project-tier fixture
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `UPDATE user_session_issuers SET project_id=$1, organization_id=$2 WHERE id=$3`, project, tc.issuerOrg, issuer) //nolint:glint // notestingrawsql: legacy nullable issuer tenancy
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), `UPDATE user_sessions SET project_id=$1, organization_id=$2 WHERE id=$3`, project, tc.sessionOrg, session) //nolint:glint // notestingrawsql: legacy nullable session tenancy
			require.NoError(t, err)
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
			} else {
				require.Empty(t, listed.Items)
				requireOopsCode(t, err, oops.CodeNotFound)
				require.Empty(t, revoker.events)
			}
		})
	}
}
