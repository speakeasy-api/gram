package platformmcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dashboards"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func dashboardRefusalOf(t *testing.T, result *mcp.CallToolResult) dashboardRefusal {
	t.Helper()
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var refusal dashboardRefusal
	require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
	return refusal
}

func TestDashboardToolErrorsAreReadable(t *testing.T) {
	t.Parallel()

	const unavailable = "Dashboards are temporarily unavailable. Try again shortly."
	for name, tc := range map[string]struct {
		err     error
		code    string
		reason  string
		message string
	}{
		"built-in":           {errDashboardBuiltInReadOnly, "built_in_read_only", "", "Speakeasy-built dashboards are read-only. Use duplicate_dashboard to make a project copy, then change the copy."},
		"stale version":      {oops.E(oops.CodeConflict, dashboards.ErrStaleVersion, "the dashboard changed since it was read; read it again"), "conflict", "", "The dashboard changed since it was read. Call get_dashboard again, preview again and confirm the new version."},
		"preview mismatch":   {errPreviewMismatch, "conflict", "", "The preview token does not match this proposal and version. Preview again and confirm the identical proposal."},
		"preview used":       {errPreviewUsed, "preview_used", "", "This preview was already confirmed under another idempotency_key. Retry with that key to see its result, or preview again to make another change."},
		"preview expired":    {errPreviewExpired, "preview_expired", "", "This preview has expired. Preview the change again and confirm what that preview shows."},
		"forbidden":          {ErrForbidden, "permission_denied", "", "An authorized external user with project:read on the exact project is required."},
		"layout rule":        {oops.E(oops.CodeBadRequest, nil, "operations[0] overlaps card x"), "invalid_request", "operations[0] overlaps card x", "Correct what the reason names and preview again."},
		"not creator":        {oops.C(oops.CodeForbidden), widgetOwnerOrWriteRequired, "", "Only the dashboard's creator, or someone with project:write, may change it."},
		"not found":          {oops.E(oops.CodeNotFound, nil, "dashboard not found"), "not_found", "dashboard not found", "Use the ids list_dashboards and get_dashboard return."},
		"unavailable":        {ErrUnavailable, unavailableCode, "", unavailable},
		"unexpected failure": {fmt.Errorf("query dashboards: %w", errors.New("connection reset by 10.0.0.7")), unavailableCode, "", unavailable},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			result, ok := dashboardToolError(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))(tc.err)
			require.True(t, ok)
			refusal := dashboardRefusalOf(t, result)
			require.Equal(t, dashboardRefusal{Code: tc.code, Feature: dashboardsFeature, Reason: tc.reason, Message: tc.message}, refusal)
			if name == "unexpected failure" {
				require.Contains(t, logs.String(), "10.0.0.7", "and logged")
			}
		})
	}
}

func TestDashboardToolsWithoutAServiceAnswerUnavailable(t *testing.T) {
	t.Parallel()

	reg := newRegistrar(newTestMCPServer())
	registerDashboardTools(reg, nil)
	ctx := contextWithPrincipal(t.Context(), testPrincipal())
	project := uuid.NewString()
	// A create, a copy and an added card make another row per confirmed key.
	idempotent := map[string]bool{
		listDashboardsToolName: true, getDashboardToolName: true, deleteDashboardToolName: true, saveDashboardFiltersToolName: true,
		saveDashboardToolName: false, duplicateDashboardToolName: false, editDashboardLayoutToolName: false,
	}
	for name, arguments := range map[string]string{
		listDashboardsToolName:       `{"project_id":"` + project + `"}`,
		getDashboardToolName:         `{"project_id":"` + project + `","dashboard_id":"mcp-tools"}`,
		saveDashboardToolName:        `{"project_id":"` + project + `","name":"Usage","confirmed":false}`,
		duplicateDashboardToolName:   `{"project_id":"` + project + `","dashboard_id":"mcp-tools","confirmed":false}`,
		deleteDashboardToolName:      `{"project_id":"` + project + `","dashboard_id":"` + project + `","confirmed":false}`,
		editDashboardLayoutToolName:  `{"project_id":"` + project + `","dashboard_id":"` + project + `","operations":[{"op":"remove_card","placement_id":"` + project + `"}],"confirmed":false}`,
		saveDashboardFiltersToolName: `{"project_id":"` + project + `","dashboard_id":"` + project + `","filters":{},"confirmed":false}`,
	} {
		descriptor := descriptorByName(t, reg, name)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences, name)
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization, name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope, name)
		// A creator changes their own dashboard with project:read alone, so
		// every tool is discoverable with it.
		require.Equal(t, discoveryProjectRead, descriptor.Meta.DiscoveryScopes, name)
		require.Equal(t, idempotent[name], descriptor.Annotations.IdempotentHint, name)
		if name == listDashboardsToolName || name == getDashboardToolName {
			require.True(t, descriptor.Annotations.ReadOnlyHint, name)
		} else {
			require.False(t, descriptor.Annotations.ReadOnlyHint, name)
			require.Contains(t, string(descriptor.InputSchema), `"idempotency_key"`, name)
			require.Contains(t, string(descriptor.InputSchema), `"expected_version"`, name)
		}
		if name == editDashboardLayoutToolName {
			// The schema says what move_card needs, so an agent learns it
			// there rather than from a refusal.
			schema := string(descriptor.InputSchema)
			require.Contains(t, schema, "move_card (placement_id with all four of x, y, w and h)")
			require.Equal(t, 4, strings.Count(schema, "move_card needs all four of x, y, w and h"), "said on each of x, y, w and h")
		}

		_, err := descriptor.Invoke(ctx, json.RawMessage(arguments))
		refusal, ok := errors.AsType[*ToolRefusalError](err)
		require.True(t, ok, "%s: %v", name, err)
		require.JSONEq(t, `{"code":"`+unavailableCode+`","feature":"dashboards","message":"Dashboards are temporarily unavailable. Try again shortly."}`, refusal.Payload, name)
	}
}
