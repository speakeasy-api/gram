package modelkeys_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/model_keys"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// newResolverUnderTest builds a Resolver sharing the test instance's database
// and encryption client, plus the org/project the auth context points at.
// Keys are seeded through the service's own upsert handler.
func newResolverUnderTest(t *testing.T, ctx context.Context, ti *testInstance) (*modelkeys.Resolver, string, string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	resolver := modelkeys.NewResolver(ti.conn, ti.enc, ti.provisioner)
	return resolver, authCtx.ActiveOrganizationID, authCtx.ProjectID.String()
}

func upsertTestKey(t *testing.T, ctx context.Context, ti *testInstance, slot string, apiKey string) {
	t.Helper()

	_, err := ti.service.UpsertKey(ctx, newUpsertPayload(slot, func(p *gen.UpsertKeyPayload) {
		p.APIKey = apiKey
	}))
	require.NoError(t, err)
}

func TestResolveKey_NoCustomerKeysFallsBackToPlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "platform-key", resolved.Key)
	require.False(t, resolved.Customer)
}

func TestResolveKey_DefaultSlotCoversEverySlot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	upsertTestKey(t, ctx, ti, modelkeys.SlotDefault, "sk-or-project-default")

	for _, slot := range []billing.ModelUsageSource{billing.ModelUsageSourceAssistants, billing.ModelUsageSourcePlayground, ""} {
		resolved, err := resolver.ResolveKey(ctx, orgID, projectID, slot, openrouter.KeyTypeChat)
		require.NoError(t, err)
		require.Equal(t, "sk-or-project-default", resolved.Key, "slot %q", slot)
		require.True(t, resolved.Customer, "slot %q", slot)
	}
}

func TestResolveKey_SlotOverrideBeatsDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	upsertTestKey(t, ctx, ti, modelkeys.SlotDefault, "sk-or-project-default")
	upsertTestKey(t, ctx, ti, string(billing.ModelUsageSourceAssistants), "sk-or-assistants-override")

	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "sk-or-assistants-override", resolved.Key)

	// The override is scoped to its slot only; other slots keep the default.
	resolved, err = resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourcePlayground, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "sk-or-project-default", resolved.Key)
}

func TestResolveKey_InternalKeyTypeStaysOnPlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	upsertTestKey(t, ctx, ti, modelkeys.SlotDefault, "sk-or-project-default")

	// Internal completions outside the exposed judge slots (chat titles,
	// segment analysis, …) must not be captured by the project default key.
	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceRiskAnalysis, openrouter.KeyTypeInternal)
	require.NoError(t, err)
	require.Equal(t, "platform-key", resolved.Key)
	require.False(t, resolved.Customer)
}

func TestResolveKey_JudgeSlotsResolveIndependently(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	upsertTestKey(t, ctx, ti, modelkeys.SlotDefault, "sk-or-project-default")
	upsertTestKey(t, ctx, ti, string(billing.ModelUsageSourceRiskPolicy), "sk-or-risk-policy-override")

	// The risk-policy judge resolves its dedicated override.
	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceRiskPolicy, openrouter.KeyTypeInternal)
	require.NoError(t, err)
	require.Equal(t, "sk-or-risk-policy-override", resolved.Key)
	require.True(t, resolved.Customer)

	// The prompt-injection judge has no override and falls back to the
	// project default key, not the risk-policy override.
	resolved, err = resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourcePromptInjection, openrouter.KeyTypeInternal)
	require.NoError(t, err)
	require.Equal(t, "sk-or-project-default", resolved.Key)
	require.True(t, resolved.Customer)
}

func TestResolveKey_JudgeSlotsWithoutKeysStayOnPlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourcePromptInjection, openrouter.KeyTypeInternal)
	require.NoError(t, err)
	require.Equal(t, "platform-key", resolved.Key)
	require.False(t, resolved.Customer)
}

func TestResolveKey_DisabledKeyIsIgnored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	_, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, func(p *gen.UpsertKeyPayload) {
		p.APIKey = "sk-or-disabled"
		p.Enabled = false
	}))
	require.NoError(t, err)

	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "platform-key", resolved.Key)
}

func TestResolveKey_DisabledSlotOverrideFallsBackToProjectDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	upsertTestKey(t, ctx, ti, modelkeys.SlotDefault, "sk-or-project-default")
	override, err := ti.service.UpsertKey(ctx, newUpsertPayload(string(billing.ModelUsageSourceAssistants), func(payload *gen.UpsertKeyPayload) {
		payload.APIKey = "sk-or-assistants-override"
		payload.Enabled = false
	}))
	require.NoError(t, err)

	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "sk-or-project-default", resolved.Key)
	require.True(t, resolved.Customer)

	_, err = ti.service.SetKeyEnabled(ctx, newSetKeyEnabledPayload(override.ID, true))
	require.NoError(t, err)

	resolved, err = resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "sk-or-assistants-override", resolved.Key)
}

func TestResolveKey_DeletedKeyIsIgnored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, projectID := newResolverUnderTest(t, ctx, ti)

	created, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)

	err = ti.service.DeleteKey(ctx, &gen.DeleteKeyPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	resolved, err := resolver.ResolveKey(ctx, orgID, projectID, billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "platform-key", resolved.Key)
}

func TestResolveKey_NoProjectFallsBackToPlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)
	resolver, orgID, _ := newResolverUnderTest(t, ctx, ti)

	upsertTestKey(t, ctx, ti, modelkeys.SlotDefault, "sk-or-project-default")

	resolved, err := resolver.ResolveKey(ctx, orgID, "", billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.NoError(t, err)
	require.Equal(t, "platform-key", resolved.Key)
}

func TestResolveKey_MalformedProjectIDErrors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	resolver, orgID, _ := newResolverUnderTest(t, ctx, ti)

	_, err := resolver.ResolveKey(ctx, orgID, "not-a-uuid", billing.ModelUsageSourceAssistants, openrouter.KeyTypeChat)
	require.ErrorContains(t, err, "invalid project id")
}
