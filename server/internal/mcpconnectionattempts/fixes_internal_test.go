package mcpconnectionattempts

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
)

func TestAdminFixesAreAdminReasons(t *testing.T) {
	t.Parallel()
	for reason := range adminFixes {
		meaning, ok := MeaningOf(reason)
		require.True(t, ok, reason)
		require.Equal(t, WhoAdmin, meaning.WhoFixes, reason)
	}
}

func TestEveryAdminFixActionCountsAsAChange(t *testing.T) {
	t.Parallel()
	for reason, fix := range adminFixes {
		for _, a := range fix.changes {
			require.True(t, IsConfigChange(a.Action), "%s credits %s", reason, a.Action)
		}
	}
	for _, action := range []audit.Action{
		audit.ActionAccessChallengeResolve,
		audit.ActionKillswitchDeactivate,
		audit.ActionKillswitchExpire,
		audit.ActionOrganizationPaygActivated,
		audit.ActionWorkloadAdmissionAdmit,
	} {
		require.True(t, IsConfigChange(string(action)), action)
	}
}
