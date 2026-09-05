package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestSetPluginAssignmentsOutputProjectsOnlySafeFields(t *testing.T) {
	t.Parallel()

	count := NewSubjectCount(8)
	output := SetPluginAssignmentsOutput{
		SetPluginAssignmentsReceiptResult: SetPluginAssignmentsReceiptResult{
			ProjectID: "00000000-0000-0000-0000-000000000001",
			Plugin: PluginAssignmentMutationPlugin{
				ID: "00000000-0000-0000-0000-000000000002", Name: "Shared Tools", Slug: "shared-tools",
				Assignments: PluginAssignmentSummary{Roles: 1}, Publication: PluginPublicationPublished,
			},
			AssignmentVersion: "opaque-version",
			Assignments:       []PluginAssignmentSummaryResult{{Kind: "role", DisplayName: "Engineering", MemberCount: &count}},
			ResultCategory:    "updated",
		},
		Receipt: RiskMutationToolReceipt{ID: "00000000-0000-0000-0000-000000000003"},
	}

	keys := decodeKeys(t, output)
	require.ElementsMatch(t, []string{
		"project_id", "plugin", "id", "name", "slug", "is_default", "assignments", "all_members", "roles", "users", "publication",
		"assignment_version", "assignments", "kind", "display_name", "member_count", "result_category", "receipt", "id", "replayed",
	}, keys)
	payload, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "principal")
	require.NotContains(t, string(payload), "reference")
	require.NotContains(t, string(payload), "description")
}

func TestPluginAssignmentMutationReceiptResultValidation(t *testing.T) {
	t.Parallel()

	result := SetPluginAssignmentsReceiptResult{
		ProjectID: uuid.NewString(),
		Plugin: PluginAssignmentMutationPlugin{
			ID: uuid.NewString(), Name: "Shared", Slug: "shared", Assignments: PluginAssignmentSummary{AllMembers: true}, Publication: PluginPublicationPublished,
		},
		AssignmentVersion: "opaque",
		Assignments:       []PluginAssignmentSummaryResult{{Kind: "everyone", DisplayName: "Everyone"}},
		ResultCategory:    "updated",
	}
	require.True(t, validPluginAssignmentReceiptResult(result))
	payload, err := encodePluginAssignmentReceiptResult(result)
	require.NoError(t, err)
	require.True(t, validPluginAssignmentReceiptPayload(payload))

	invalid := result
	invalid.Assignments[0].Kind = "email"
	require.False(t, validPluginAssignmentReceiptResult(invalid))
	_, err = encodePluginAssignmentReceiptResult(invalid)
	require.ErrorIs(t, err, ErrPluginAssignmentMutationUnavailable)
}

func TestPluginAssignmentMutationInputHashCoversExactNormalizedWrite(t *testing.T) {
	t.Parallel()

	projectID, pluginID := uuid.New(), uuid.New()
	empty, err := normalizePluginAssignmentReferences(nil)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
	references, err := normalizePluginAssignmentReferences([]string{" ref-b ", "ref-a", "ref-b"})
	require.NoError(t, err)
	require.Equal(t, []string{"ref-a", "ref-b"}, references)
	base := normalizedPluginAssignmentMutationInput(projectID, pluginID.String(), references, "version")
	first, err := pluginAssignmentMutationInputHash(base)
	require.NoError(t, err)
	second, err := pluginAssignmentMutationInputHash(base)
	require.NoError(t, err)
	require.Equal(t, first, second)

	changed := base
	changed.ExpectedAssignmentVersion = "other"
	other, err := pluginAssignmentMutationInputHash(changed)
	require.NoError(t, err)
	require.NotEqual(t, first, other)

	_, err = normalizePluginAssignmentReferences([]string{" "})
	require.ErrorIs(t, err, ErrPluginAssignmentMutationInvalid)
}

func TestResolveMutationAssignmentsSkipsEmptyChoiceLookup(t *testing.T) {
	t.Parallel()

	principalURNs, summaries, err := (&PluginsService{}).resolveMutationAssignments(t.Context(), nil, Principal{}, ResolvedProject{}, nil)
	require.NoError(t, err)
	require.NotNil(t, principalURNs)
	require.Empty(t, principalURNs)
	require.NotNil(t, summaries)
	require.Empty(t, summaries)
}

func TestPluginAssignmentMutationToolRequiresConfirmation(t *testing.T) {
	t.Parallel()

	err := requirePluginAssignmentConfirmation(false)
	var mutation *PluginAssignmentMutationError
	require.ErrorAs(t, err, &mutation)
	require.Equal(t, "confirmation_required", mutation.Code)
	require.NoError(t, requirePluginAssignmentConfirmation(true))

	service := NewPluginsService(nil, OperationBudget{}, "")
	_, err = service.SetPluginAssignments(t.Context(), Principal{}, SetPluginAssignmentsInput{})
	require.ErrorIs(t, err, ErrPluginAssignmentMutationUnavailable)

	refusal, ok := pluginToolResult(&PluginAssignmentMutationError{Code: "confirmation_required", Message: "confirm", Cause: ErrPluginAssignmentMutationInvalid})
	require.True(t, ok)
	require.True(t, refusal.IsError)
	text, ok := refusal.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "confirmation_required")
}
