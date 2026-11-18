package agent_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/inferenceprofiles/repo"
)

func TestGetPlugins_ToolConfigFollowsInferenceProfileRules(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	queries := repo.New(ti.conn)

	before, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.Nil(t, before.ToolConfig, "no rules means no tool_config")

	profile, err := queries.CreateInferenceProfile(ctx, repo.CreateInferenceProfileParams{
		OrganizationID: ti.orgID,
		Name:           "Bedrock",
		Provider:       "bedrock",
		Settings:       []byte(`{"region":"us-west-2","aws_profile":"dev-bedrock"}`),
	})
	require.NoError(t, err)

	unchanged, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.Equal(t, before.Etag, unchanged.Etag, "a profile no rule uses changes nothing")

	rule, err := queries.CreateInferenceProfileRule(ctx, repo.CreateInferenceProfileRuleParams{
		OrganizationID: ti.orgID,
		ProfileID:      profile.ID,
		PrincipalUrn:   "email:" + mockidp.MockUserEmail,
		Harness:        pgtype.Text{String: "claude_code", Valid: true},
		Position:       0,
	})
	require.NoError(t, err)

	withRule, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.NotEqual(t, before.Etag, withRule.Etag)
	require.Equal(t, map[string]any{
		"claude_code": map[string]any{
			"env": map[string]any{
				"CLAUDE_CODE_USE_BEDROCK": "1",
				"AWS_REGION":              "us-west-2",
				"AWS_PROFILE":             "dev-bedrock",
			},
			"awsAuthRefresh": "aws sso login --profile dev-bedrock",
		},
	}, withRule.ToolConfig)

	// Someone else's poll is untouched by this person's rule.
	other, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("someone-" + uuid.NewString()[:8] + "@example.com")})
	require.NoError(t, err)
	require.Nil(t, other.ToolConfig)

	// Widening the rule to every harness changes the poll again.
	_, err = queries.UpdateInferenceProfileRule(ctx, repo.UpdateInferenceProfileRuleParams{
		ProfileID:      profile.ID,
		PrincipalUrn:   rule.PrincipalUrn,
		Harness:        pgtype.Text{String: "", Valid: false},
		Position:       0,
		OrganizationID: ti.orgID,
		ID:             rule.ID,
	})
	require.NoError(t, err)
	widened, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.NotEqual(t, withRule.Etag, widened.Etag)
	require.Contains(t, widened.ToolConfig, "codex")

	// Deleting the rule removes tool_config and restores the original ETag.
	_, err = queries.DeleteInferenceProfileRule(ctx, repo.DeleteInferenceProfileRuleParams{OrganizationID: ti.orgID, ID: rule.ID})
	require.NoError(t, err)
	removed, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.Nil(t, removed.ToolConfig)
	require.Equal(t, before.Etag, removed.Etag)
}
