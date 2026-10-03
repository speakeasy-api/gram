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
			d, human, reason, err := assistantidentity.CaptureSlackDelegation(ctx, f.db, f.org, "TEXAMPLE", "UEXAMPLE")
			require.NoError(t, err)
			require.NotNil(t, d)
			require.Equal(t, f.actor, human)
			require.Empty(t, reason)
			e := assistantidentity.Execution{Identity: assistantidentity.Identity{OrganizationID: f.org}, HumanUserID: human, Slack: d}
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
			if change == "revoke" || change == "inactive" || change == "bot" || change == "conflict" {
				_, _, _, err = assistantidentity.CaptureSlackDelegation(ctx, f.db, f.org, "TEXAMPLE", "UEXAMPLE")
				require.ErrorIs(t, err, assistantidentity.ErrActorIneligible, "known ineligible mapping is not workload fallback")
			}
		})
	}
}

func TestSlackExecutionAbsentMapping(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	d, human, reason, err := assistantidentity.CaptureSlackDelegation(t.Context(), f.db, f.org, "TEXAMPLE", "UABSENT")
	require.NoError(t, err)
	require.Nil(t, d)
	require.Empty(t, human)
	require.Equal(t, "slack_mapping_absent", reason)
}
