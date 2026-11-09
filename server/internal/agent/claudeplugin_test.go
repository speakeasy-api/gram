package agent_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// stubClaudePlugins records the member and project each render was for.
type stubClaudePlugins struct {
	userID    string
	projectID uuid.UUID
	orgSlug   string
}

func (s *stubClaudePlugins) ClaudeWebObservabilityPlugin(_ context.Context, _, orgSlug string, projectID uuid.UUID, _, userID string) ([]byte, error) {
	s.userID, s.projectID, s.orgSlug = userID, projectID, orgSlug
	return []byte("zip"), nil
}

func TestGetClaudeObservabilityPlugin_RendersForKeyOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	stub := &stubClaudePlugins{}
	ti.service.WithClaudeObservabilityPlugins(stub)
	ctx = withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)

	res, body, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	require.Equal(t, "zip", string(data))
	require.Equal(t, "application/zip", res.ContentType)
	require.Equal(t, mockidp.MockOrgSlug, stub.orgSlug, "the archive is named for the org's real slug")
	require.Equal(t, `attachment; filename="`+mockidp.MockOrgSlug+`-observability-claude.zip"`, res.ContentDisposition)
	require.Equal(t, mockidp.MockUserID, stub.userID, "the key is minted for the key's owner")
	require.Equal(t, ti.projectID, stub.projectID)
}

// The org install key is shared by every device in the org, so it cannot
// prove which member is asking; accepting it would let any holder mint a key
// attributed to any member and revoke theirs.
func TestGetClaudeObservabilityPlugin_RefusesOrgInstallKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	stub := &stubClaudePlugins{}
	ti.service.WithClaudeObservabilityPlugins(stub)

	_, _, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{})
	requireCode(t, err, oops.CodeForbidden)
	require.Empty(t, stub.userID, "no key is minted for the org install key")
}

func TestGetClaudeObservabilityPlugin_RefusesNonMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	stub := &stubClaudePlugins{}
	ti.service.WithClaudeObservabilityPlugins(stub)
	ctx = withPerUserKeyAuth(t, ctx, "stranger@example.com")

	_, _, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{})
	requireCode(t, err, oops.CodeForbidden)
	require.Empty(t, stub.userID, "no key is minted for a non-member")
}

func TestGetClaudeObservabilityPlugin_RefusesAgentIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	stub := &stubClaudePlugins{}
	ti.service.WithClaudeObservabilityPlugins(stub)
	agentCtx, _ := withAgentKeyAuth(t, ctx, ti, "CI agent")

	_, _, err := ti.service.GetClaudeObservabilityPlugin(agentCtx, &gen.GetClaudeObservabilityPluginPayload{})
	requireCode(t, err, oops.CodeForbidden)
	require.Empty(t, stub.userID, "no key is minted for an agent identity")
}

func TestGetClaudeObservabilityPlugin_NotFoundWithoutObservabilityPlugin(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, ctx context.Context, ti *testInstance){
		"unpublished": func(t *testing.T, _ context.Context, _ *testInstance) {
			t.Helper()
		},
		"empty marketplace token": func(t *testing.T, ctx context.Context, ti *testInstance) {
			t.Helper()
			publishMarketplace(t, ctx, ti.conn, ti.projectID, "")
		},
		"disabled": func(t *testing.T, ctx context.Context, ti *testInstance) {
			t.Helper()
			publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
			disableObservability(t, ctx, ti.conn, ti.projectID)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAgentService(t)
			setup(t, ctx, ti)
			stub := &stubClaudePlugins{}
			ti.service.WithClaudeObservabilityPlugins(stub)
			ctx = withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)

			_, _, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{})
			requireCode(t, err, oops.CodeNotFound)
			require.Empty(t, stub.userID)
		})
	}
}

// seedClaudeWebKey inserts a key shaped like the Claude web plugin download's,
// owned by the mock user, and returns its id.
func seedClaudeWebKey(t *testing.T, ctx context.Context, ti *testInstance, name, prefix string) uuid.UUID {
	t.Helper()
	key, err := keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  ti.orgID,
		Name:            name,
		KeyHash:         "hash-" + name,
		KeyPrefix:       prefix,
		Scopes:          []string{"hooks"},
		CreatedByUserID: mockidp.MockUserID,
		ProjectID:       uuid.NullUUID{UUID: ti.projectID, Valid: true},
	})
	require.NoError(t, err)
	return key.ID
}

func TestGetClaudeObservabilityPluginStatus_NoDownloadYet(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	ti.service.WithClaudeObservabilityPlugins(&stubClaudePlugins{})
	// A hooks key the member named themselves is not the plugin's.
	seedClaudeWebKey(t, ctx, ti, "my hooks key", "gram_live_abcde")
	ctx = withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)

	res, err := ti.service.GetClaudeObservabilityPluginStatus(ctx, &gen.GetClaudeObservabilityPluginStatusPayload{})
	require.NoError(t, err)
	require.True(t, res.Available)
	require.Nil(t, res.KeyCreatedAt)
	require.Nil(t, res.KeyLastUsedAt)
}

func TestGetClaudeObservabilityPluginStatus_ReportsKeyUse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	ti.service.WithClaudeObservabilityPlugins(&stubClaudePlugins{})
	id := seedClaudeWebKey(t, ctx, ti, "plugins-claude-web-hooks-20261109-120000-abcdef", "gram_live_abcde")
	ctx = withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)

	res, err := ti.service.GetClaudeObservabilityPluginStatus(ctx, &gen.GetClaudeObservabilityPluginStatusPayload{})
	require.NoError(t, err)
	require.True(t, res.Available)
	require.NotNil(t, res.KeyCreatedAt, "a downloaded plugin reports when its key was minted")
	require.Nil(t, res.KeyLastUsedAt, "no hooks have used the key yet")

	require.NoError(t, keysrepo.New(ti.conn).UpdateAPIKeyLastAccessedAt(ctx, id))
	res, err = ti.service.GetClaudeObservabilityPluginStatus(ctx, &gen.GetClaudeObservabilityPluginStatusPayload{})
	require.NoError(t, err)
	require.NotNil(t, res.KeyLastUsedAt, "hooks authenticating with the key mark the plugin as sending events")
	_, err = time.Parse(time.RFC3339, *res.KeyLastUsedAt)
	require.NoError(t, err)
}

func TestGetClaudeObservabilityPluginStatus_UnavailableWhenObservabilityOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	disableObservability(t, ctx, ti.conn, ti.projectID)
	ti.service.WithClaudeObservabilityPlugins(&stubClaudePlugins{})
	seedClaudeWebKey(t, ctx, ti, "plugins-claude-web-hooks-20261109-120000-abcdef", "gram_live_abcde")
	ctx = withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)

	res, err := ti.service.GetClaudeObservabilityPluginStatus(ctx, &gen.GetClaudeObservabilityPluginStatusPayload{})
	require.NoError(t, err)
	require.False(t, res.Available)
	require.Nil(t, res.KeyCreatedAt, "an org that turned observability off is not offered the plugin")
}

func TestGetClaudeObservabilityPluginStatus_RefusesOrgInstallKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	ti.service.WithClaudeObservabilityPlugins(&stubClaudePlugins{})

	_, err := ti.service.GetClaudeObservabilityPluginStatus(ctx, &gen.GetClaudeObservabilityPluginStatusPayload{})
	requireCode(t, err, oops.CodeForbidden)
}

func TestGetClaudeObservabilityPluginStatus_RefusesAgentIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	ti.service.WithClaudeObservabilityPlugins(&stubClaudePlugins{})
	agentCtx, _ := withAgentKeyAuth(t, ctx, ti, "CI agent")

	_, err := ti.service.GetClaudeObservabilityPluginStatus(agentCtx, &gen.GetClaudeObservabilityPluginStatusPayload{})
	requireCode(t, err, oops.CodeForbidden)
}
