package platformmcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageAttributionOutputsProjectOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	window, err := resolveWindow("24h", now, drilldownWindowSpec)
	require.NoError(t, err)
	envelope := newDataEnvelope(now, now.Add(-time.Minute), window, true)

	mcpUsers := ListMCPUsageUsersOutput{
		ProjectID: "project",
		MCPID:     "mcp",
		Envelope:  envelope,
		Users: []MCPUsageUser{{
			SubjectReference: "opaque",
			MaskedIdentity:   "a***@e***",
			Activity:         "mixed",
			Errors:           "observed",
			Blocked:          "none_observed",
			LastUsedAt:       now.Format(time.RFC3339),
		}},
		Truncated: false,
	}
	require.ElementsMatch(t, []string{
		"project_id", "mcp_id",
		"data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"users", "subject_reference", "masked_identity", "activity", "errors", "blocked", "last_used_at", "truncated",
	}, decodeKeys(t, mcpUsers))

	skills := QuerySkillUsageOutput{
		ProjectID: "project",
		Envelope:  envelope,
		Skills: []SkillUsage{{
			SkillName: "repo-review", Activations: 7, ActiveUsers: NewSubjectCount(6), Errors: "not_recorded",
		}},
		Truncated: false,
	}
	require.ElementsMatch(t, []string{
		"project_id", "data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"skills", "skill_name", "activations", "active_users", "errors", "truncated",
	}, decodeKeys(t, skills))

	skillUsers := ListSkillUsageUsersOutput{
		ProjectID: "project",
		SkillName: "repo-review",
		Envelope:  envelope,
		Users: []SkillUsageUser{{
			SubjectReference: "opaque", MaskedIdentity: "a***@e***", Activity: "observed", Errors: "not_recorded",
		}},
		Truncated: false,
	}
	require.ElementsMatch(t, []string{
		"project_id", "skill_name", "data", "queried_at", "data_through", "freshness", "no_observations", "resolved_window", "window", "from", "to",
		"users", "subject_reference", "masked_identity", "activity", "errors", "truncated",
	}, decodeKeys(t, skillUsers))
}

func TestUsageAttributionCategoriesDoNotExposeCounts(t *testing.T) {
	t.Parallel()

	require.Equal(t, "mixed", subjectActivity(true, true))
	require.Equal(t, "errors_only", subjectActivity(false, true))
	require.Equal(t, "successful", subjectActivity(true, false))
	require.Equal(t, "observed", subjectActivity(false, false))
	require.Equal(t, "observed", subjectErrors(true))
	require.Equal(t, "none_observed", subjectErrors(false))
	require.Equal(t, "observed", subjectBlocked(true))
	require.Equal(t, "none_observed", subjectBlocked(false))
}

func TestUsageAttributionReferenceScopesBindTargetAndWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	oneHour, err := resolveWindow("1h", now, drilldownWindowSpec)
	require.NoError(t, err)
	lastDay, err := resolveWindow("24h", now, drilldownWindowSpec)
	require.NoError(t, err)

	first := drilldownTarget{projectID: "project", mcpServerID: "mcp-a", window: oneHour}
	second := drilldownTarget{projectID: "project", mcpServerID: "mcp-b", window: oneHour}
	require.NotEqual(t, mcpUsageUserScope(first), mcpUsageUserScope(second))
	first.window = lastDay
	require.NotEqual(t, mcpUsageUserScope(first), mcpUsageUserScope(drilldownTarget{projectID: "project", mcpServerID: "mcp-a", window: oneHour}))
	require.NotEqual(t, skillUsageUserScope("project", "skill-a", oneHour), skillUsageUserScope("project", "skill-b", oneHour))
	require.NotEqual(t, skillUsageUserScope("project", "skill-a", oneHour), skillUsageUserScope("project", "skill-a", lastDay))
}
