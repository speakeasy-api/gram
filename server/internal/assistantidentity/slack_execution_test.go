package assistantidentity_test

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSlackExecutionProvenanceRevocation(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"revision", "inactive", "bot", "conflict", "revoke", "disconnect", "wrong tenant", "wrong sender", "wrong human"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			ctx := t.Context()
			q := repo.New(f.db)
			require.NoError(t, q.FixtureSlackExecutionMapping(ctx, repo.FixtureSlackExecutionMappingParams{UserID: f.actor, OrganizationID: f.org, SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE", Generation: uuid.New()}))
			row, err := q.GetSlackExecutionMapping(ctx, repo.GetSlackExecutionMappingParams{OrganizationID: f.org, SlackTeamID: "TEXAMPLE", SlackUserID: "UEXAMPLE"})
			require.NoError(t, err)
			d := &assistantidentity.SlackDelegation{TeamID: "TEXAMPLE", UserID: "UEXAMPLE", MembershipID: row.MembershipID, MappingID: row.MappingID, MappingRevision: row.MappingRevision, ConnectionGeneration: row.ConnectionGeneration}
			e := assistantidentity.Execution{Identity: assistantidentity.Identity{OrganizationID: f.org}, HumanUserID: row.UserID, Slack: d}
			require.NoError(t, assistantidentity.ValidateSlackDelegation(ctx, f.db, e))
			p := repo.FixtureInvalidateSlackExecutionMembershipParams{OrganizationID: f.org, SlackTeamID: d.TeamID, SlackUserID: d.UserID, Status: "active", MemberType: "person", MappingRevision: 1, ConflictReason: pgtype.Text{}}
			switch change {
			case "revision":
				p.MappingRevision++
			case "inactive":
				p.Status = "deactivated"
			case "bot":
				p.MemberType = "bot"
			case "conflict":
				p.ConflictReason = pgtype.Text{String: "changed", Valid: true}
			case "revoke":
				require.NoError(t, slackrepo.New(f.db).RevokeSlackIdentityMapping(ctx, slackrepo.RevokeSlackIdentityMappingParams{OrganizationID: f.org, SlackTeamID: d.TeamID, SlackUserID: d.UserID}))
			case "disconnect":
				e.Slack.ConnectionGeneration = uuid.New()
			case "wrong tenant":
				e.Identity.OrganizationID = "org-other"
			case "wrong sender":
				e.Slack.UserID = "UOTHER"
			case "wrong human":
				e.HumanUserID = "other"
			}
			require.NoError(t, q.FixtureInvalidateSlackExecutionMembership(ctx, p))
			require.ErrorIs(t, assistantidentity.ValidateSlackDelegation(ctx, f.db, e), assistantidentity.ErrActorIneligible)
		})
	}
}

func TestSlackExecutionValidationRejectsAbsentMapping(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	e := assistantidentity.Execution{Identity: assistantidentity.Identity{OrganizationID: f.org}, HumanUserID: f.actor, Slack: &assistantidentity.SlackDelegation{TeamID: "TEXAMPLE", UserID: "UABSENT", MembershipID: uuid.New(), MappingID: uuid.New(), MappingRevision: 1, ConnectionGeneration: uuid.New()}}
	require.ErrorIs(t, assistantidentity.ValidateSlackDelegation(t.Context(), f.db, e), assistantidentity.ErrActorIneligible)
}
