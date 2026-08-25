package metamcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func seedMetaMcpServer(t *testing.T, ctx context.Context, ti *testInstance, name string) *types.MetaMcpServer {
	t.Helper()

	created, err := ti.service.CreateMetaMcpServer(ctx, &gen.CreateMetaMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                name,
		UserSessionIssuerID: nil,
	})
	require.NoError(t, err)

	return created
}

func TestAddMetaMcpMember_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "member host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerAddMember)
	require.NoError(t, err)

	member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        conv.PtrEmpty(3),
	})
	require.NoError(t, err)
	require.NotEmpty(t, member.ID)
	require.Equal(t, serverID.String(), member.McpServerID)
	require.Equal(t, 3, member.SortOrder)
	require.NotNil(t, member.McpServerName)
	require.NotNil(t, member.McpServerSlug)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMetaMcpServerAddMember)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestAddMetaMcpMember_DuplicateConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "duplicate host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)

	_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestAddMetaMcpMember_ServerMayJoinMultipleMetas(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	first := seedMetaMcpServer(t, ctx, ti, "first host")
	second := seedMetaMcpServer(t, ctx, ti, "second host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	for _, meta := range []*types.MetaMcpServer{first, second} {
		_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			MetaMcpServerID:  meta.ID,
			McpServerID:      serverID.String(),
			SortOrder:        nil,
		})
		require.NoError(t, err)
	}
}

func TestAddMetaMcpMember_RejectsSecondServerOnSameBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tests := []struct {
		name    string
		backend mcpserversrepo.CreateMCPServerParams
	}{
		{
			name:    "remote",
			backend: mcpserversrepo.CreateMCPServerParams{RemoteMcpServerID: conv.ToNullUUID(seedRemoteBackend(t, ctx, ti.conn, *authCtx.ProjectID))},
		},
		{
			name:    "tunnel",
			backend: mcpserversrepo.CreateMCPServerParams{TunneledMcpServerID: conv.ToNullUUID(seedTunnelBackend(t, ctx, ti.conn, *authCtx.ProjectID))},
		},
		{
			name:    "toolset",
			backend: mcpserversrepo.CreateMCPServerParams{ToolsetID: conv.ToNullUUID(seedToolsetBackend(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID))},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			meta := seedMetaMcpServer(t, ctx, ti, "shared backend host "+tt.name)
			backend := tt.backend
			first := seedMcpServerFronting(t, ctx, ti.conn, *authCtx.ProjectID, backend)
			second := seedMcpServerFronting(t, ctx, ti.conn, *authCtx.ProjectID, backend)

			_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
				SessionToken:     nil,
				ApikeyToken:      nil,
				ProjectSlugInput: nil,
				MetaMcpServerID:  meta.ID,
				McpServerID:      first.String(),
				SortOrder:        nil,
			})
			require.NoError(t, err)

			_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
				SessionToken:     nil,
				ApikeyToken:      nil,
				ProjectSlugInput: nil,
				MetaMcpServerID:  meta.ID,
				McpServerID:      second.String(),
				SortOrder:        nil,
			})
			requireOopsCode(t, err, oops.CodeConflict)
		})
	}
}

func TestAddMetaMcpMember_AllowsDistinctBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "distinct backend host")

	// One server per backend kind: every member leaves three of the four
	// backend columns null, so a null-matching guard would reject these.
	servers := []uuid.UUID{
		seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID),
		seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID),
		seedMcpServerFronting(t, ctx, ti.conn, *authCtx.ProjectID, mcpserversrepo.CreateMCPServerParams{
			TunneledMcpServerID: conv.ToNullUUID(seedTunnelBackend(t, ctx, ti.conn, *authCtx.ProjectID)),
		}),
		seedMcpServerFronting(t, ctx, ti.conn, *authCtx.ProjectID, mcpserversrepo.CreateMCPServerParams{
			ToolsetID: conv.ToNullUUID(seedToolsetBackend(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)),
		}),
		seedMcpServerFronting(t, ctx, ti.conn, *authCtx.ProjectID, mcpserversrepo.CreateMCPServerParams{
			UnproxiedMcpServerID: conv.ToNullUUID(seedUnproxiedBackend(t, ctx, ti.conn, *authCtx.ProjectID)),
		}),
	}

	for _, serverID := range servers {
		_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			MetaMcpServerID:  meta.ID,
			McpServerID:      serverID.String(),
			SortOrder:        nil,
		})
		require.NoError(t, err)
	}
}

func TestAddMetaMcpMember_RemovedMemberFreesBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "freed backend host")
	backend := mcpserversrepo.CreateMCPServerParams{
		RemoteMcpServerID: conv.ToNullUUID(seedRemoteBackend(t, ctx, ti.conn, *authCtx.ProjectID)),
	}
	first := seedMcpServerFronting(t, ctx, ti.conn, *authCtx.ProjectID, backend)
	second := seedMcpServerFronting(t, ctx, ti.conn, *authCtx.ProjectID, backend)

	member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      first.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               member.ID,
	}))

	_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      second.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)
}

func TestAddMetaMcpMember_RejectsForeignProjectServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "tenancy host")
	otherProjectID := seedOtherProject(t, ctx, ti.conn, authCtx.ActiveOrganizationID)
	foreignServerID := seedMcpServer(t, ctx, ti.conn, otherProjectID)

	_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      foreignServerID.String(),
		SortOrder:        nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestAddMetaMcpMember_RejectsDeletedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "tombstone host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)

	_, err = ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestAddMetaMcpMember_UnknownMetaNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  uuid.NewString(),
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestAddMetaMcpMember_ReAddAfterRemove(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "re-add host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	member, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.RemoveMetaMcpMember(ctx, &gen.RemoveMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               member.ID,
	}))

	readded, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		MetaMcpServerID:  meta.ID,
		McpServerID:      serverID.String(),
		SortOrder:        nil,
	})
	require.NoError(t, err)
	require.NotEqual(t, member.ID, readded.ID, "re-adding creates a fresh membership row")
}

func TestAddMetaMcpMember_ConcurrentDuplicateAdds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	meta := seedMetaMcpServer(t, ctx, ti, "race host")
	serverID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := ti.service.AddMetaMcpMember(ctx, &gen.AddMetaMcpMemberPayload{
				SessionToken:     nil,
				ApikeyToken:      nil,
				ProjectSlugInput: nil,
				MetaMcpServerID:  meta.ID,
				McpServerID:      serverID.String(),
				SortOrder:        nil,
			})
			results <- err
		}()
	}

	var successes, conflicts int
	for range 2 {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeConflict, oopsErr.Code)
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
}
