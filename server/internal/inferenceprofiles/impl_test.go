package inferenceprofiles_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/inference_profiles"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func bedrockSettings() *gen.InferenceProfileSettings {
	return &gen.InferenceProfileSettings{
		Region:       new("us-west-2"),
		AwsProfile:   new("dev-bedrock"),
		GcpProjectID: nil,
		BaseURL:      nil,
		Models:       &gen.InferenceProfileModels{Default: new("us.anthropic.claude-sonnet-4-5-20250929-v1:0"), Opus: nil, Sonnet: nil, Haiku: nil},
	}
}

func vertexSettings() *gen.InferenceProfileSettings {
	return &gen.InferenceProfileSettings{Region: new("us-east5"), AwsProfile: nil, GcpProjectID: new("acme-ai-prod"), BaseURL: nil, Models: nil}
}

func gatewaySettings() *gen.InferenceProfileSettings {
	return &gen.InferenceProfileSettings{Region: nil, AwsProfile: nil, GcpProjectID: nil, BaseURL: new("https://llm.example.com/v1"), Models: nil}
}

func createProfile(t *testing.T, ctx context.Context, ti *testInstance, name, provider string, settings *gen.InferenceProfileSettings) *gen.InferenceProfile {
	t.Helper()
	profile, err := ti.service.CreateProfile(ctx, &gen.CreateProfilePayload{SessionToken: nil, Name: name, Provider: provider, Settings: settings})
	require.NoError(t, err)
	return profile
}

func createRule(t *testing.T, ctx context.Context, ti *testInstance, profileID, principal string, harness *string, position int) *gen.InferenceProfileRule {
	t.Helper()
	rule, err := ti.service.CreateRule(ctx, &gen.CreateRulePayload{SessionToken: nil, ProfileID: profileID, PrincipalUrn: principal, Harness: harness, Position: position})
	require.NoError(t, err)
	return rule
}

func auditCount(t *testing.T, ctx context.Context, ti *testInstance, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
	require.NoError(t, err)
	return count
}

func TestProfileLifecycle_RecordsAuditEvents(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	profile := createProfile(t, ctx, ti, "Bedrock dev", "bedrock", bedrockSettings())
	require.Equal(t, "bedrock", profile.Provider)
	require.Equal(t, "us-west-2", *profile.Settings.Region)
	require.Equal(t, int64(1), auditCount(t, ctx, ti, audit.ActionInferenceProfileCreate))

	updated, err := ti.service.UpdateProfile(ctx, &gen.UpdateProfilePayload{SessionToken: nil, ID: profile.ID, Name: "Gateway", Provider: "gateway", Settings: gatewaySettings()})
	require.NoError(t, err)
	require.Equal(t, "gateway", updated.Provider)
	require.Nil(t, updated.Settings.Region)
	require.Equal(t, int64(1), auditCount(t, ctx, ti, audit.ActionInferenceProfileUpdate))

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionInferenceProfileUpdate)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "bedrock", before["Provider"])

	listed, err := ti.service.ListProfiles(ctx, &gen.ListProfilesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Profiles, 1)

	require.NoError(t, ti.service.DeleteProfile(ctx, &gen.DeleteProfilePayload{SessionToken: nil, ID: profile.ID}))
	require.Equal(t, int64(1), auditCount(t, ctx, ti, audit.ActionInferenceProfileDelete))
	listed, err = ti.service.ListProfiles(ctx, &gen.ListProfilesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Profiles)

	requireOopsCode(t, ti.service.DeleteProfile(ctx, &gen.DeleteProfilePayload{SessionToken: nil, ID: profile.ID}), oops.CodeNotFound)
}

func TestDeleteProfile_DeletesAndAuditsItsRules(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	profile := createProfile(t, ctx, ti, "Bedrock", "bedrock", bedrockSettings())
	other := createProfile(t, ctx, ti, "Gateway", "gateway", gatewaySettings())
	createRule(t, ctx, ti, profile.ID, "user:all", nil, 0)
	createRule(t, ctx, ti, profile.ID, "email:dev@example.com", new("codex"), 0)
	kept := createRule(t, ctx, ti, other.ID, "email:dev@example.com", new("claude_code"), 0)

	beforeDeletes := auditCount(t, ctx, ti, audit.ActionInferenceProfileRuleDelete)
	require.NoError(t, ti.service.DeleteProfile(ctx, &gen.DeleteProfilePayload{SessionToken: nil, ID: profile.ID}))
	require.Equal(t, beforeDeletes+2, auditCount(t, ctx, ti, audit.ActionInferenceProfileRuleDelete))

	rules, err := ti.service.ListRules(ctx, &gen.ListRulesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, rules.Rules, 1)
	require.Equal(t, kept.ID, rules.Rules[0].ID)
}

func TestRuleLifecycle_RecordsAuditEventsAndReorders(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	profile := createProfile(t, ctx, ti, "Bedrock", "bedrock", bedrockSettings())
	first := createRule(t, ctx, ti, profile.ID, "user:all", nil, 0)
	second := createRule(t, ctx, ti, profile.ID, "email:Dev@Example.com", nil, 1)
	require.Equal(t, "email:dev@example.com", second.PrincipalUrn, "email principals are normalized")
	require.Equal(t, int64(2), auditCount(t, ctx, ti, audit.ActionInferenceProfileRuleCreate))

	_, err := ti.service.UpdateRule(ctx, &gen.UpdateRulePayload{SessionToken: nil, ID: second.ID, ProfileID: profile.ID, PrincipalUrn: second.PrincipalUrn, Harness: nil, Position: 0})
	require.NoError(t, err)
	_, err = ti.service.UpdateRule(ctx, &gen.UpdateRulePayload{SessionToken: nil, ID: first.ID, ProfileID: profile.ID, PrincipalUrn: first.PrincipalUrn, Harness: new("claude_code"), Position: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), auditCount(t, ctx, ti, audit.ActionInferenceProfileRuleUpdate))

	rules, err := ti.service.ListRules(ctx, &gen.ListRulesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, rules.Rules, 2)
	require.Equal(t, second.ID, rules.Rules[0].ID)
	require.Equal(t, first.ID, rules.Rules[1].ID)
	require.Equal(t, "claude_code", *rules.Rules[1].Harness)

	require.NoError(t, ti.service.DeleteRule(ctx, &gen.DeleteRulePayload{SessionToken: nil, ID: first.ID}))
	require.Equal(t, int64(1), auditCount(t, ctx, ti, audit.ActionInferenceProfileRuleDelete))
	requireOopsCode(t, ti.service.DeleteRule(ctx, &gen.DeleteRulePayload{SessionToken: nil, ID: first.ID}), oops.CodeNotFound)
}

func TestValidationErrors(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateProfile(ctx, &gen.CreateProfilePayload{SessionToken: nil, Name: "Plain HTTP", Provider: "gateway", Settings: &gen.InferenceProfileSettings{Region: nil, AwsProfile: nil, GcpProjectID: nil, BaseURL: new("http://llm.example.com"), Models: nil}})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateProfile(ctx, &gen.CreateProfilePayload{SessionToken: nil, Name: "Bad region", Provider: "bedrock", Settings: &gen.InferenceProfileSettings{Region: new("us west"), AwsProfile: nil, GcpProjectID: nil, BaseURL: nil, Models: nil}})
	requireOopsCode(t, err, oops.CodeBadRequest)

	vertex := createProfile(t, ctx, ti, "Vertex", "vertex", vertexSettings())
	_, err = ti.service.CreateProfile(ctx, &gen.CreateProfilePayload{SessionToken: nil, Name: "vertex", Provider: "vertex", Settings: vertexSettings()})
	requireOopsCode(t, err, oops.CodeConflict)

	// Codex has no Vertex provider.
	_, err = ti.service.CreateRule(ctx, &gen.CreateRulePayload{SessionToken: nil, ProfileID: vertex.ID, PrincipalUrn: "user:all", Harness: new("codex"), Position: 0})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateRule(ctx, &gen.CreateRulePayload{SessionToken: nil, ProfileID: vertex.ID, PrincipalUrn: "agent:" + uuid.NewString(), Harness: nil, Position: 0})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateRule(ctx, &gen.CreateRulePayload{SessionToken: nil, ProfileID: vertex.ID, PrincipalUrn: "user:user_not_a_member", Harness: nil, Position: 0})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateRule(ctx, &gen.CreateRulePayload{SessionToken: nil, ProfileID: uuid.NewString(), PrincipalUrn: "user:all", Harness: nil, Position: 0})
	requireOopsCode(t, err, oops.CodeBadRequest)

	createRule(t, ctx, ti, vertex.ID, "user:all", nil, 0)
	_, err = ti.service.CreateRule(ctx, &gen.CreateRulePayload{SessionToken: nil, ProfileID: vertex.ID, PrincipalUrn: "user:all", Harness: nil, Position: 1})
	requireOopsCode(t, err, oops.CodeConflict)

	// A Codex rule pins its Bedrock profile's provider away from Vertex.
	bedrock := createProfile(t, ctx, ti, "Bedrock", "bedrock", bedrockSettings())
	createRule(t, ctx, ti, bedrock.ID, "user:all", new("codex"), 0)
	_, err = ti.service.UpdateProfile(ctx, &gen.UpdateProfilePayload{SessionToken: nil, ID: bedrock.ID, Name: "Bedrock", Provider: "vertex", Settings: vertexSettings()})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestNonAdminIsForbidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	profile := createProfile(t, ctx, ti, "Bedrock", "bedrock", bedrockSettings())

	member := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))
	before, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	_, err = ti.service.ListProfiles(member, &gen.ListProfilesPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateProfile(member, &gen.CreateProfilePayload{SessionToken: nil, Name: "x", Provider: "bedrock", Settings: bedrockSettings()})
	requireOopsCode(t, err, oops.CodeForbidden)
	requireOopsCode(t, ti.service.DeleteProfile(member, &gen.DeleteProfilePayload{SessionToken: nil, ID: profile.ID}), oops.CodeForbidden)
	_, err = ti.service.CreateRule(member, &gen.CreateRulePayload{SessionToken: nil, ProfileID: profile.ID, PrincipalUrn: "user:all", Harness: nil, Position: 0})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListRules(member, &gen.ListRulesPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Preview(member, &gen.PreviewPayload{SessionToken: nil, Email: ti.email})
	requireOopsCode(t, err, oops.CodeForbidden)

	after, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestPreview_ResolvesPerHarness(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	bedrock := createProfile(t, ctx, ti, "Bedrock", "bedrock", bedrockSettings())
	gateway := createProfile(t, ctx, ti, "Gateway", "gateway", gatewaySettings())
	everyone := createRule(t, ctx, ti, gateway.ID, "user:all", nil, 0)
	mine := createRule(t, ctx, ti, bedrock.ID, "user:"+ti.userID, new("claude_code"), 0)

	preview, err := ti.service.Preview(ctx, &gen.PreviewPayload{SessionToken: nil, Email: ti.email})
	require.NoError(t, err)
	require.Len(t, preview.Resolutions, 2)

	byHarness := map[string]*gen.InferenceProfileResolution{}
	for _, resolution := range preview.Resolutions {
		byHarness[resolution.Harness] = resolution
	}
	require.Equal(t, bedrock.ID, byHarness["claude_code"].Profile.ID)
	require.Equal(t, mine.ID, byHarness["claude_code"].Rule.ID)
	require.Equal(t, gateway.ID, byHarness["codex"].Profile.ID)
	require.Equal(t, everyone.ID, byHarness["codex"].Rule.ID)

	require.Contains(t, preview.ToolConfig, "claude_code")
	require.Contains(t, preview.ToolConfig, "codex")

	// user:all reaches members only, so an address outside the organization
	// resolves to nothing.
	outsider, err := ti.service.Preview(ctx, &gen.PreviewPayload{SessionToken: nil, Email: "outsider@example.com"})
	require.NoError(t, err)
	for _, resolution := range outsider.Resolutions {
		require.Nil(t, resolution.Profile)
	}
	require.Empty(t, outsider.ToolConfig)
}
