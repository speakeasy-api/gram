package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformslack "github.com/speakeasy-api/gram/server/internal/platformtools/slack"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The managed assistant must expose the full observability catalog the old
// client-side AI Insights copilot had — not just search_logs. This guards
// against the gap silently reopening if a tool is dropped from the bundle.
func TestManagedAssistantLogsToolsExposesObservabilityCatalog(t *testing.T) {
	t.Parallel()

	tools := ManagedAssistantLogsTools(nil)

	got := make([]string, 0, len(tools))
	for _, tool := range tools {
		got = append(got, tool.Executor.Descriptor().Name)
	}

	require.ElementsMatch(t, []string{
		"platform_search_logs",
		"platform_search_tool_calls",
		"platform_get_tool_usage_summary",
		"platform_search_chats",
		"platform_search_users",
		"platform_get_project_metrics_summary",
		"platform_get_user_metrics_summary",
		"platform_get_observability_overview",
		"platform_list_attribute_keys",
	}, got)
}

func TestManagedAssistantLogsToolsDescribesTunneledMCPUsage(t *testing.T) {
	t.Parallel()

	tools := ManagedAssistantLogsTools(nil)
	for _, tool := range tools {
		if tool.Executor.Descriptor().Name != "platform_get_tool_usage_summary" {
			continue
		}
		require.Contains(t, tool.Executor.Descriptor().Description, "tunneled_mcp_server")
		return
	}

	require.Fail(t, "platform_get_tool_usage_summary not registered")
}

func TestManagedAssistantChatsToolsExposesCatalog(t *testing.T) {
	t.Parallel()

	got := toolNames(ManagedAssistantChatsTools(nil))
	require.ElementsMatch(t, []string{
		"platform_list_chats",
		"platform_load_chat",
	}, got)
}

func TestManagedAssistantUsersToolsExposesCatalog(t *testing.T) {
	t.Parallel()

	got := toolNames(ManagedAssistantUsersTools(nil))
	require.ElementsMatch(t, []string{
		"platform_list_organization_users",
	}, got)
}

func TestManagedAssistantRiskToolsExposesCatalog(t *testing.T) {
	t.Parallel()

	got := toolNames(ManagedAssistantRiskTools(nil, "test-redaction-key"))
	require.ElementsMatch(t, []string{
		"platform_list_risk_policies",
		"platform_list_risk_results_for_agent",
		"platform_list_risk_results_by_chat",
		"platform_get_risk_policy_status",
		"platform_get_risk_rule_breakdown",
		"platform_list_risk_exclusions",
		"platform_create_risk_exclusion",
		"platform_mark_risk_false_positive",
		"platform_unmark_risk_false_positive",
	}, got)
}

// Providers reject tool names longer than 64 characters, and the assistant
// runtime prefixes every platform tool with `mcp__<server-slug>_` (25 chars
// for the managed assistant). A name over this budget doesn't degrade — the
// whole turn fails with a schema error before the model sees anything.
func TestManagedAssistantToolNamesFitProviderLimit(t *testing.T) {
	t.Parallel()

	const maxNameLen = 40

	tools := ManagedAssistantRiskTools(nil, "test-redaction-key")
	tools = append(tools, ManagedAssistantLogsTools(nil)...)
	tools = append(tools, ManagedAssistantChatsTools(nil)...)
	tools = append(tools, ManagedAssistantUsersTools(nil)...)
	tools = append(tools, ManagedAssistantDeploymentsTools(nil)...)

	for _, name := range toolNames(tools) {
		require.LessOrEqual(t, len(name), maxNameLen, "tool name %q is too long once prefixed", name)
	}
}

func TestManagedAssistantDeploymentsToolsExposesCatalog(t *testing.T) {
	t.Parallel()

	got := toolNames(ManagedAssistantDeploymentsTools(nil))
	require.ElementsMatch(t, []string{
		"platform_get_deployment_logs",
	}, got)
}

func TestManagedAssistantSkillsToolsExposesCatalog(t *testing.T) {
	t.Parallel()

	tools := ManagedAssistantSkillsTools(nil, nil)
	require.ElementsMatch(t, []string{
		"platform_create_skill",
		"platform_list_skills",
		"platform_get_skill",
		"platform_list_skill_versions",
		"platform_list_skill_distributions",
		"platform_distribute_skill",
		"platform_undistribute_skill",
		"platform_skill_insights",
	}, toolNames(tools))
	for _, tool := range tools {
		require.Equal(t, "skills", tool.RequiredFeature)
	}
}

func TestManagedAssistantPluginsToolsExposesCatalog(t *testing.T) {
	t.Parallel()

	tools := ManagedAssistantPluginsTools(nil)
	require.ElementsMatch(t, []string{
		"platform_list_plugins",
	}, toolNames(tools))
	// The plugin catalog is not part of the skills feature; it stays visible so
	// distribution targets can be resolved regardless of feature state.
	for _, tool := range tools {
		require.Empty(t, tool.RequiredFeature)
	}
}

func TestAssistantSkillToolsExposesCatalog(t *testing.T) {
	t.Parallel()

	tools := AssistantSkillTools(testenv.NewLogger(t), nil, nil)
	require.Equal(t, []string{
		platformtools.ToolNameSkillsLoad,
		platformtools.ToolNamePlatformSkillFeedback,
	}, toolNames(tools))
	for _, tool := range tools {
		require.Empty(t, tool.RequiredFeature)
	}
}

func toolNames(tools []platformtools.ExternalTool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, tool.Executor.Descriptor().Name)
	}
	return out
}

type stubDirectExecutor struct {
	response string
	err      error
	called   bool
	gotBody  string
}

func (s *stubDirectExecutor) Call(_ context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	s.called = true
	if payload != nil {
		body, err := io.ReadAll(payload)
		if err != nil {
			return fmt.Errorf("read payload: %w", err)
		}
		s.gotBody = string(body)
	}
	if s.err != nil {
		return s.err
	}
	if _, err := io.WriteString(wr, s.response); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

func TestService_ExecuteTool_RequiresProjectAuthContext(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	projectID := uuid.New()

	_, err := svc.ExecuteTool(context.Background(), &gateway.ToolCallPlan{
		Kind: gateway.ToolKindPlatform,
		Descriptor: &gateway.ToolDescriptor{
			ProjectID: projectID.String(),
			URN:       urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs"),
		},
		Platform: &gateway.PlatformToolCallPlan{},
	}, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
	}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "project auth context")
}

func TestService_ExecuteTool_RejectsMismatchedProjectAuthContext(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	descriptorProjectID := uuid.New()
	authProjectID := uuid.New()
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ProjectID: &authProjectID,
	})

	_, err := svc.ExecuteTool(ctx, &gateway.ToolCallPlan{
		Kind: gateway.ToolKindPlatform,
		Descriptor: &gateway.ToolDescriptor{
			ProjectID: descriptorProjectID.String(),
			URN:       urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs"),
		},
		Platform: &gateway.PlatformToolCallPlan{},
	}, toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
	}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "does not match project")
}

// overridePlan builds a plan whose URN is unregistered, so any test that
// reaches the URN registry instead of the pinned executor would 404.
func overridePlan(projectID uuid.UUID, exec gateway.PlatformDirectExecutor) *gateway.ToolCallPlan {
	return &gateway.ToolCallPlan{
		Kind: gateway.ToolKindPlatform,
		Descriptor: &gateway.ToolDescriptor{
			ProjectID: projectID.String(),
			URN:       urn.NewTool(urn.ToolKindPlatform, "unregistered", "tool"),
		},
		Platform: &gateway.PlatformToolCallPlan{Executor: exec},
	}
}

func TestService_ExecuteTool_UsesPlanExecutorOverride(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-1",
		ProjectID:            &projectID,
	})

	exec := &stubDirectExecutor{response: `{"ok":true}`}

	result, err := svc.ExecuteTool(ctx, overridePlan(projectID, exec), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
	}, strings.NewReader(`{"hello":"world"}`))
	require.NoError(t, err)
	require.True(t, exec.called)
	require.JSONEq(t, `{"hello":"world"}`, exec.gotBody)
	require.NotNil(t, result)
	require.JSONEq(t, `{"ok":true}`, string(result.Body))
}

func TestService_ExecuteTool_PlanExecutorOverrideSurfacesError(t *testing.T) {
	t.Parallel()

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-1",
		ProjectID:            &projectID,
	})

	exec := &stubDirectExecutor{err: errors.New("boom")}

	_, err := svc.ExecuteTool(ctx, overridePlan(projectID, exec), toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
	}, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
	require.True(t, exec.called)
}

// slackRedirectTransport sends a Slack tool's outbound call to a local test
// server, since the tools pin the real slack.com base URL.
type slackRedirectTransport struct {
	host string
}

func (t *slackRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rewritten := req.Clone(req.Context())
	rewritten.URL.Scheme = "http"
	rewritten.URL.Host = t.host

	resp, err := http.DefaultTransport.RoundTrip(rewritten)
	if err != nil {
		return nil, fmt.Errorf("roundtrip to test slack: %w", err)
	}
	return resp, nil
}

// Slack answers a thread timestamp that names no thread with HTTP 200 and an
// ok=false envelope. That refusal must remain attributable to the caller after
// the runtime wraps it, so the MCP boundary logs it at warn instead of driving
// the component's error rate with an ordinary caller mistake.
func TestService_ExecuteTool_KeepsSlackRefusalAttributedToCaller(t *testing.T) {
	t.Parallel()

	slack := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":false,"error":"thread_not_found"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer slack.Close()

	slackURL, err := url.Parse(slack.URL)
	require.NoError(t, err)

	svc := NewService(testenv.NewLogger(t), nil, nil, audit.NewLogger())
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-1",
		ProjectID:            &projectID,
	})

	tool := platformslack.NewReadThreadMessagesTool(&http.Client{
		Transport: &slackRedirectTransport{host: slackURL.Host},
	})

	env := toolconfig.ToolCallEnv{
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
	}
	env.SystemEnv.Set("SLACK_BOT_TOKEN", "xoxb-test")

	_, err = svc.ExecuteTool(ctx, overridePlan(projectID, tool), env, strings.NewReader(`{"channel_id":"C1","thread_ts":"1.2"}`))
	require.ErrorContains(t, err, "thread_not_found")
	require.True(t, oops.IsClientFault(err), "a thread the caller named that does not exist is the caller's to fix")
}
