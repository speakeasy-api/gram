package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// createTunneledBackedMcpServer creates an mcp_servers row backed by the given
// tunneled source and returns its ID. Several rows may share one source.
func createTunneledBackedMcpServer(t *testing.T, ctx context.Context, ti *testInstance, tunneledID uuid.UUID) string {
	t.Helper()

	tunneled := tunneledID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		Name:                "tunneled tool metadata server " + uuid.NewString(),
		EnvironmentID:       nil,
		RemoteMcpServerID:   nil,
		TunneledMcpServerID: &tunneled,
		ToolsetID:           nil,
		Visibility:          types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	return created.ID
}

func newTunneledToolMetadataServer(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	return createTunneledBackedMcpServer(t, ctx, ti, seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID))
}

func metadataForm(name string, readOnly, destructive *bool) *gen.ToolMetadataForm {
	return &gen.ToolMetadataForm{ToolName: name, Title: nil, ReadOnlyHint: readOnly, DestructiveHint: destructive, IdempotentHint: nil, OpenWorldHint: nil}
}

func listTunneledToolMetadata(t *testing.T, ctx context.Context, ti *testInstance, serverID string) []*types.ToolMetadata {
	t.Helper()

	listed, err := ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      serverID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	return listed.Tools
}

func toolNames(tools []*types.ToolMetadata) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.ToolName)
	}
	return names
}

// Every metadata method accepts a tunneled-backed server, and each committed
// write is reflected by the disposition resolver at once rather than after the
// cache TTL, because the cache is keyed by the mcp_servers id whatever the
// backend.
func TestToolMetadata_TunneledBackedServerSupportsAllMethods(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	serverID := newTunneledToolMetadataServer(t, ctx, ti)

	// No metadata resolves to no dispositions, and warms the cache.
	resolved, err := ti.dispositions.Dispositions(ctx, serverID, projectID)
	require.NoError(t, err)
	require.Empty(t, resolved)

	beforeUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)

	added, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      serverID,
		Tools:            []*gen.ToolMetadataForm{metadataForm("list_devices", new(true), nil)},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, added.Tools, 1)

	resolved, err = ti.dispositions.Dispositions(ctx, serverID, projectID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"list_devices": "read_only"}, resolved)

	replaced, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			metadataForm("list_devices", new(true), nil),
			metadataForm("wipe_device", new(false), new(true)),
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, replaced.Tools, 2)

	resolved, err = ti.dispositions.Dispositions(ctx, serverID, projectID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"list_devices": "read_only", "wipe_device": "destructive"}, resolved)

	updated, err := ti.service.SetToolMetadata(ctx, &gen.SetToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "list_devices",
		Title:            nil,
		ReadOnlyHint:     nil,
		DestructiveHint:  new(true),
		IdempotentHint:   nil,
		OpenWorldHint:    nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, new(true), updated.DestructiveHint)

	resolved, err = ti.dispositions.Dispositions(ctx, serverID, projectID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"list_devices": "destructive", "wipe_device": "destructive"}, resolved)

	err = ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "wipe_device",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	resolved, err = ti.dispositions.Dispositions(ctx, serverID, projectID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"list_devices": "destructive"}, resolved)

	require.Equal(t, []string{"list_devices"}, toolNames(listTunneledToolMetadata(t, ctx, ti, serverID)))

	afterUpdates, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerToolMetadataUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeUpdates+4, afterUpdates)
}

// Several MCP servers may front one tunnel. Metadata is keyed by the
// mcp_servers id, so each keeps its own inventory and dispositions.
func TestToolMetadata_TunneledServersSharingATunnelStayIsolated(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	tunneledID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
	first := createTunneledBackedMcpServer(t, ctx, ti, tunneledID)
	second := createTunneledBackedMcpServer(t, ctx, ti, tunneledID)

	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      first,
		Tools:            []*gen.ToolMetadataForm{metadataForm("run_report", new(true), nil)},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      second,
		Tools:            []*gen.ToolMetadataForm{metadataForm("run_report", new(false), new(true))},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	firstDispositions, err := ti.dispositions.Dispositions(ctx, first, projectID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"run_report": "read_only"}, firstDispositions)

	secondDispositions, err := ti.dispositions.Dispositions(ctx, second, projectID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"run_report": "destructive"}, secondDispositions)

	// Replacing one server's whole collection leaves the other's untouched.
	_, err = ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
		McpServerID:      first,
		Tools:            []*gen.ToolMetadataForm{},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Empty(t, listTunneledToolMetadata(t, ctx, ti, first))
	require.Equal(t, []string{"run_report"}, toolNames(listTunneledToolMetadata(t, ctx, ti, second)))

	secondDispositions, err = ti.dispositions.Dispositions(ctx, second, projectID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"run_report": "destructive"}, secondDispositions)
}

// toolMetadataMutator runs one of the four metadata writes against serverID.
type toolMetadataMutator struct {
	name string
	run  func(ctx context.Context, ti *testInstance, serverID string) error
}

var toolMetadataMutators = []toolMetadataMutator{
	{name: "set batch", run: func(ctx context.Context, ti *testInstance, serverID string) error {
		_, err := ti.service.SetToolMetadataBatch(ctx, &gen.SetToolMetadataBatchPayload{
			McpServerID:      serverID,
			Tools:            []*gen.ToolMetadataForm{},
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		return err
	}},
	{name: "add batch", run: func(ctx context.Context, ti *testInstance, serverID string) error {
		_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
			McpServerID:      serverID,
			Tools:            []*gen.ToolMetadataForm{metadataForm("injected", new(true), nil)},
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		return err
	}},
	{name: "set", run: func(ctx context.Context, ti *testInstance, serverID string) error {
		_, err := ti.service.SetToolMetadata(ctx, &gen.SetToolMetadataPayload{
			McpServerID:      serverID,
			ToolName:         "list_devices",
			Title:            nil,
			ReadOnlyHint:     nil,
			DestructiveHint:  new(true),
			IdempotentHint:   nil,
			OpenWorldHint:    nil,
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		return err
	}},
	{name: "delete", run: func(ctx context.Context, ti *testInstance, serverID string) error {
		return ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
			McpServerID:      serverID,
			ToolName:         "list_devices",
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
	}},
}

// seedListDevices stores one read-only tool, which every refused mutator below
// must leave exactly as it was.
func seedListDevices(t *testing.T, ctx context.Context, ti *testInstance, serverID string) {
	t.Helper()

	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID:      serverID,
		Tools:            []*gen.ToolMetadataForm{metadataForm("list_devices", new(true), nil)},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func requireListDevicesUnchanged(t *testing.T, ctx context.Context, ti *testInstance, serverID string, projectID string) {
	t.Helper()

	tools := listTunneledToolMetadata(t, ctx, ti, serverID)
	require.Equal(t, []string{"list_devices"}, toolNames(tools))
	require.Equal(t, new(true), tools[0].ReadOnlyHint)
	require.Nil(t, tools[0].DestructiveHint)

	resolved, err := ti.dispositions.Dispositions(ctx, serverID, projectID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"list_devices": "read_only"}, resolved)
}

// A caller with no grant, or with write access to only a sibling server on the
// same tunnel, cannot change a tunneled server's metadata through any write.
func TestToolMetadata_TunneledMutationsRequireWriteOnThatServer(t *testing.T) {
	t.Parallel()

	for _, mutator := range toolMetadataMutators {
		t.Run(mutator.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := authCtx.ProjectID.String()

			tunneledID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)
			serverID := createTunneledBackedMcpServer(t, ctx, ti, tunneledID)
			siblingID := createTunneledBackedMcpServer(t, ctx, ti, tunneledID)
			seedListDevices(t, ctx, ti, serverID)

			beforeAudit, err := audittest.AuditLogCount(ctx, ti.conn)
			require.NoError(t, err)

			deniedCtx := withExactAuthzGrants(t, ctx, ti.conn)
			requireOopsCode(t, mutator.run(deniedCtx, ti, serverID), oops.CodeForbidden)

			readOnlyCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPRead, serverID))
			requireOopsCode(t, mutator.run(readOnlyCtx, ti, serverID), oops.CodeForbidden)

			siblingCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, siblingID))
			requireOopsCode(t, mutator.run(siblingCtx, ti, serverID), oops.CodeForbidden)

			afterAudit, err := audittest.AuditLogCount(ctx, ti.conn)
			require.NoError(t, err)
			require.Equal(t, beforeAudit, afterAudit)

			requireListDevicesUnchanged(t, ctx, ti, serverID, projectID)
		})
	}
}

// Metadata methods resolve the server within the caller's project, so a
// tunneled server in another project of the same organization is not found
// and keeps its metadata, even for an organization admin.
func TestToolMetadata_TunneledMutationsCannotCrossProjects(t *testing.T) {
	t.Parallel()

	for _, mutator := range toolMetadataMutators {
		t.Run(mutator.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
				Name: "Other project", Slug: "other-project-" + uuid.NewString()[:8], OrganizationID: authCtx.ActiveOrganizationID,
			})
			require.NoError(t, err)

			otherAuth := *authCtx
			otherAuth.ProjectID = &otherProject.ID
			otherCtx := contextvalues.SetAuthContext(ctx, &otherAuth)

			serverID := createTunneledBackedMcpServer(t, otherCtx, ti, seedTunneledMcpServer(t, ctx, ti.conn, otherProject.ID))
			seedListDevices(t, otherCtx, ti, serverID)

			beforeAudit, err := audittest.AuditLogCount(ctx, ti.conn)
			require.NoError(t, err)

			requireOopsCode(t, mutator.run(ctx, ti, serverID), oops.CodeNotFound)

			afterAudit, err := audittest.AuditLogCount(ctx, ti.conn)
			require.NoError(t, err)
			require.Equal(t, beforeAudit, afterAudit)

			requireListDevicesUnchanged(t, otherCtx, ti, serverID, otherProject.ID.String())
		})
	}
}

// Unproxied servers carry no Speakeasy traffic to enforce dispositions on, so
// every metadata method still turns them away.
func TestToolMetadata_RejectsUnproxiedBackedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	unproxiedID := seedUnproxiedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	staffCtx := withStaffEmail(t, ctx)
	created, err := ti.service.CreateMcpServer(staffCtx, &gen.CreateMcpServerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Name:                 "unproxied tool metadata server " + uuid.NewString(),
		EnvironmentID:        nil,
		RemoteMcpServerID:    nil,
		TunneledMcpServerID:  nil,
		UnproxiedMcpServerID: &unproxiedID,
		ToolsetID:            nil,
		Visibility:           types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	for _, mutator := range toolMetadataMutators {
		requireOopsCode(t, mutator.run(ctx, ti, created.ID), oops.CodeInvalid)
	}

	_, err = ti.service.ListToolMetadata(ctx, &gen.ListToolMetadataPayload{
		McpServerID:      created.ID,
		IncludeDeleted:   nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
