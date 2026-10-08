package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func TestPluginAssignmentAdmissionErrorMappings(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		cause error
		code  string
	}{
		{cause: admission.ErrApprovalRequired, code: "approval_required"},
		{cause: admission.ErrPrivateGatewayAudience, code: "conflict"},
		{cause: admission.ErrUnavailable, code: "feature_unavailable"},
	} {
		err := pluginAssignmentAdmissionError(fmt.Errorf("guard: %w", test.cause))
		require.ErrorIs(t, err, test.cause)
		var mutation *PluginAssignmentMutationError
		require.ErrorAs(t, err, &mutation)
		require.Equal(t, test.code, mutation.Code)
		result, ok := pluginToolResult(err)
		require.True(t, ok)
		require.True(t, result.IsError)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.Contains(t, text.Text, test.code)
	}
	require.NoError(t, pluginAssignmentAdmissionError(nil))
}

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

type stubAssignmentReviewFiler struct {
	review   ShadowMCPReviewRequest
	err      error
	calls    int
	url      string
	activity string
	note     string
}

func (f *stubAssignmentReviewFiler) FileShadowMCPReview(_ context.Context, _ Principal, _ ResolvedProject, canonicalURL, activity, justification string) (ShadowMCPReviewRequest, error) {
	f.calls++
	f.url, f.activity, f.note = canonicalURL, activity, justification
	return f.review, f.err
}

func TestPluginAssignmentRefusalFilesAReviewForTheRefusedServer(t *testing.T) {
	t.Parallel()

	refused := fmt.Errorf("replace plugin assignments: %w", &admission.ApprovalRequiredError{CanonicalURL: "https://mcp.example.test/server"})
	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	principal := Principal{OrganizationID: "org", UserID: "admin"}

	filer := &stubAssignmentReviewFiler{review: ShadowMCPReviewRequest{RequestID: "request-1", Status: "requested", Target: "https://mcp.example.test/server", ReviewURL: "https://app.example.test/review", Explanation: "why"}}
	err := (&PluginsService{reviews: filer}).reviewAssignmentRefusal(t.Context(), principal, project, "Support", "", refused)
	var mutation *PluginAssignmentMutationError
	require.ErrorAs(t, err, &mutation)
	require.Equal(t, "shadow_mcp_review_requested", mutation.Code)
	require.Equal(t, "why", mutation.Message)
	var filed *shadowMCPReviewFiledError
	require.ErrorAs(t, err, &filed)
	require.Equal(t, "request-1", filed.review.RequestID)
	require.ErrorIs(t, err, admission.ErrApprovalRequired, "the original refusal stays in the chain")
	require.Equal(t, "https://mcp.example.test/server", filer.url)
	require.Equal(t, "changing who receives the Support plugin in project project", filer.activity)
	require.Empty(t, filer.note)

	result, ok := pluginToolResult(err)
	require.True(t, ok)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `"code":"shadow_mcp_review_requested"`)
	require.Contains(t, text.Text, `"review_url":"https://app.example.test/review"`)

	// A user-supplied justification is passed through for the service to record.
	_ = (&PluginsService{reviews: filer}).reviewAssignmentRefusal(t.Context(), principal, project, "Support", "for the on-call rota", refused)
	require.Equal(t, "for the on-call rota", filer.note)

	// Without a filer, or for any other error, the refusal passes through.
	require.Equal(t, refused, (&PluginsService{}).reviewAssignmentRefusal(t.Context(), principal, project, "Support", "", refused))
	other := errors.New("conflict")
	require.Equal(t, other, (&PluginsService{reviews: filer}).reviewAssignmentRefusal(t.Context(), principal, project, "Support", "", other))
	// A failed filing keeps the original refusal rather than inventing a review.
	failing := &stubAssignmentReviewFiler{err: errors.New("queue down")}
	require.Equal(t, refused, (&PluginsService{reviews: failing}).reviewAssignmentRefusal(t.Context(), principal, project, "Support", "", refused))
}
