package assistants

// Phase A expectations from main: with the server-key rollout off, saves keep hosted servers in assistant_toolsets.

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestHostedAttachmentWritesLeaveNoTwinsFlagOff(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	name := "updated"

	t.Run("toolsets write keeps the toolset row shape", func(t *testing.T) {
		t.Parallel()
		f := newPhaseAHostedAttachmentFixture(t, "hosted_write_toolsets_off")
		f.addLegacyRow(t, f.hosted.ID, uuid.NullUUID{})
		f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
		f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
		before := f.runtime(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil,
			[]*types.AssistantToolsetRef{{ToolsetSlug: "billing"}}, nil, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 1, counts.Toolsets, "older pods read hosted servers from assistant_toolsets")
		require.EqualValues(t, 1, counts.McpServers, "twin removed; remote server untouched")
		require.ElementsMatch(t, before, f.runtime(t))
	})

	t.Run("mcp servers write leaves an attached hosted server as it is", func(t *testing.T) {
		t.Parallel()
		f := newPhaseAHostedAttachmentFixture(t, "hosted_write_servers_off")
		f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
		f.addLegacyRow(t, f.legacy.ID, uuid.NullUUID{})
		before := f.runtime(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, nil,
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 1, counts.Toolsets)
		require.EqualValues(t, 1, counts.McpServers)
		require.ElementsMatch(t, before, f.runtime(t))
	})

	t.Run("older dashboard attaches a hosted server from the server picker", func(t *testing.T) {
		t.Parallel()
		f := newPhaseAHostedAttachmentFixture(t, "hosted_write_old_picker_off")
		f.addLegacyRow(t, f.legacy.ID, uuid.NullUUID{})
		view := f.view(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, view.Toolsets,
			append(view.McpServers, &types.AssistantMCPServerRef{McpServerSlug: "org-test-billing-x1"}), nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 2, counts.Toolsets, "attached in today's row shape")
		require.Zero(t, counts.McpServers)
		require.ElementsMatch(t, []string{"billing", "legacy"}, runtimeIDs(f.runtime(t)))
	})

	t.Run("mcp servers echo of an attached hosted server with MCP off is a no-op", func(t *testing.T) {
		t.Parallel()
		f := newPhaseAHostedAttachmentFixture(t, "hosted_write_echo_disabled_off")
		f.addLegacyRow(t, f.hosted.ID, f.environmentID(t, "staging"))
		f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
		f.setToolsetMCPEnabled(t, false)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, nil,
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}, {McpServerSlug: "remote"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 1, counts.Toolsets)
		require.EqualValues(t, 1, counts.McpServers)
		view := f.view(t)
		require.Len(t, view.Toolsets, 1)
		require.NotNil(t, view.Toolsets[0].EnvironmentSlug)
		require.Equal(t, "staging", *view.Toolsets[0].EnvironmentSlug, "environment not rewritten")
	})

	t.Run("echoing a read back is a no-op", func(t *testing.T) {
		t.Parallel()
		f := newPhaseAHostedAttachmentFixture(t, "hosted_write_echo_off")
		f.addLegacyRow(t, f.hosted.ID, uuid.NullUUID{})
		f.addLegacyRow(t, f.legacy.ID, uuid.NullUUID{})
		f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
		before := f.runtime(t)
		view := f.view(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, view.Toolsets, view.McpServers, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 2, counts.Toolsets)
		require.EqualValues(t, 1, counts.McpServers)
		require.ElementsMatch(t, before, f.runtime(t))
		require.Equal(t, view.McpServers, f.view(t).McpServers)
	})

	t.Run("toolsets govern hosted servers when both arrays are sent", func(t *testing.T) {
		t.Parallel()
		f := newPhaseAHostedAttachmentFixture(t, "hosted_write_both_off")
		f.addLegacyRow(t, f.hosted.ID, uuid.NullUUID{})
		staging := "staging"
		// An older dashboard echoes a hosted server in both arrays.
		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil,
			[]*types.AssistantToolsetRef{{ToolsetSlug: "billing", EnvironmentSlug: &staging}},
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		require.Equal(t, "staging", f.runtime(t)[0].Headers["Gram-Environment"])

		_, err = f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil,
			[]*types.AssistantToolsetRef{},
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}, {McpServerSlug: "remote"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		require.Equal(t, []string{"remote"}, runtimeIDs(f.runtime(t)), "removing it from toolsets detaches it")
	})

	t.Run("mcp servers write keeps unlisted hosted servers", func(t *testing.T) {
		t.Parallel()
		f := newPhaseAHostedAttachmentFixture(t, "hosted_write_keep_off")
		f.addLegacyRow(t, f.hosted.ID, uuid.NullUUID{})
		f.addServerRow(t, f.remote.ID, uuid.NullUUID{})

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, nil,
			[]*types.AssistantMCPServerRef{}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		require.Equal(t, []string{"billing"}, runtimeIDs(f.runtime(t)))
	})
}

func TestHostedAttachmentCreateByServerSlugFlagOff(t *testing.T) {
	t.Parallel()
	f := newPhaseAHostedAttachmentFixture(t, "hosted_create_by_server_off")
	record, err := f.core.CreateAssistant(t.Context(), "org-test", f.projectID, "user-1", "Second", "openai/gpt-4o-mini", "", nil,
		[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}, {McpServerSlug: "remote"}}, 300, 1, StatusActive, false, allowAllMCPEnable)
	require.NoError(t, err)
	f.assistant = record.ID
	counts := f.counts(t)
	require.EqualValues(t, 1, counts.Toolsets)
	require.EqualValues(t, 1, counts.McpServers)
	require.ElementsMatch(t, []string{"billing", "remote"}, runtimeIDs(f.runtime(t)))
}
