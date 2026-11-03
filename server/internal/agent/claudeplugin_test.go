package agent_test

import (
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
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

func TestGetClaudeObservabilityPlugin_RendersForVouchedMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	stub := &stubClaudePlugins{}
	ti.service.WithClaudeObservabilityPlugins(stub)

	res, body, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	require.Equal(t, "zip", string(data))
	require.Equal(t, "application/zip", res.ContentType)
	require.Equal(t, mockidp.MockOrgSlug, stub.orgSlug, "the archive is named for the org's real slug")
	require.Equal(t, `attachment; filename="`+mockidp.MockOrgSlug+`-observability-claude.zip"`, res.ContentDisposition)
	require.Equal(t, mockidp.MockUserID, stub.userID, "the key is minted for the vouched member")
	require.Equal(t, ti.projectID, stub.projectID)
}

func TestGetClaudeObservabilityPlugin_PerUserKeyRendersForKeyOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	stub := &stubClaudePlugins{}
	ti.service.WithClaudeObservabilityPlugins(stub)
	ctx = withPerUserKeyAuth(t, ctx, mockidp.MockUserEmail)

	_, _, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{Email: new("someone-else@example.com")})
	require.NoError(t, err)
	require.Equal(t, mockidp.MockUserID, stub.userID, "a per-user key ignores the vouched email")
}

func TestGetClaudeObservabilityPlugin_OrgKeyRequiresVouchedEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	ti.service.WithClaudeObservabilityPlugins(&stubClaudePlugins{})

	_, _, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{Email: nil})
	requireCode(t, err, oops.CodeBadRequest)
}

func TestGetClaudeObservabilityPlugin_RefusesNonMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	stub := &stubClaudePlugins{}
	ti.service.WithClaudeObservabilityPlugins(stub)

	_, _, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{Email: new("stranger@example.com")})
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

	_, _, err := ti.service.GetClaudeObservabilityPlugin(agentCtx, &gen.GetClaudeObservabilityPluginPayload{Email: new(mockidp.MockUserEmail)})
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

			_, _, err := ti.service.GetClaudeObservabilityPlugin(ctx, &gen.GetClaudeObservabilityPluginPayload{Email: new(mockidp.MockUserEmail)})
			requireCode(t, err, oops.CodeNotFound)
			require.Empty(t, stub.userID)
		})
	}
}
