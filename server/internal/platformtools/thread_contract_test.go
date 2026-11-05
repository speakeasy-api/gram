package platformtools_test

import (
	"context"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/platformtools/memory"
	"github.com/speakeasy-api/gram/server/internal/platformtools/skills"
	"github.com/speakeasy-api/gram/server/internal/platformtools/slack"
	"github.com/speakeasy-api/gram/server/internal/platformtools/triggers"
	"github.com/stretchr/testify/require"
)

func TestBuiltinThreadContextContracts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		tool   core.PlatformToolExecutor
		policy core.ThreadContextPolicy
	}{
		{memory.NewRememberTool(nil), core.ThreadContextRequired},
		{memory.NewRecallTool(nil), core.ThreadContextRequired},
		{memory.NewForgetTool(nil), core.ThreadContextRequired},
		{skills.NewLoadTool(nil, nil), core.ThreadContextRequired},
		{triggers.NewUnsubscribeThreadTool(nil), core.ThreadContextRequired},
		{triggers.NewAssistantListTriggersTool(nil, nil), core.ThreadContextRequired},
		{triggers.NewAssistantConfigureTriggerTool(nil, nil, nil), core.ThreadContextRequired},
		{triggers.NewListTriggersTool(nil, nil), ""},
		{triggers.NewConfigureTriggerTool(nil, nil, nil), core.ThreadContextWake},
		{slack.NewSendMessageTool(nil, nil), core.ThreadContextSlackSelf},
		{slack.NewReadThreadMessagesTool(nil), ""},
	} {
		t.Run(tc.tool.Descriptor().Name+string(tc.policy), func(t *testing.T) {
			t.Parallel()
			desc := tc.tool.Descriptor()
			require.Equal(t, tc.policy, desc.ThreadContext)
			listed := platformtools.DescriptorForContext(context.Background(), desc)
			if tc.policy == "" {
				require.Equal(t, desc.InputSchema, listed.InputSchema)
			} else {
				require.Contains(t, string(listed.InputSchema), `"thread_id"`)
			}
		})
	}
}
