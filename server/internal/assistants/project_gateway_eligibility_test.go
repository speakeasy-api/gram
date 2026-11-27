package assistants

import (
	"context"
	"encoding/json"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type stubProjectGatewayEvaluator struct {
	calls atomic.Int32
	eval  func(ctx context.Context, orgID string, projectID uuid.UUID, turnToken string, serverURL *url.URL) (orggateway.ProjectGatewayEvaluation, error)
}

func (s *stubProjectGatewayEvaluator) EvaluateProjectGatewayForTurnCredential(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	turnToken string,
	serverURL *url.URL,
) (orggateway.ProjectGatewayEvaluation, error) {
	s.calls.Add(1)
	return s.eval(ctx, organizationID, projectID, turnToken, serverURL)
}

func TestEvaluateProjectGatewayEligibility_SetAndEnvEquality(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	serverURL := mustParseURLForServiceTest(t, "https://api.example.test")
	orgID := "org-gw-elig"
	projectID := uuid.New()
	assistantID := uuid.New()
	hostedID := uuid.New()
	remoteID := uuid.New()
	extraID := uuid.New()
	gatewayURL := orggateway.ProjectURL(serverURL, "acme", "proj")

	attached := []assistantMCPServerRow{
		{
			MCPServerID:            hostedID,
			ToolsetSlug:            pgtype.Text{String: "billing", Valid: true},
			Visibility:             visibility.Private,
			EndpointSlug:           "billing",
			DefaultEnvironmentSlug: pgtype.Text{String: "prod", Valid: true},
		},
		{
			MCPServerID:  remoteID,
			ServerSlug:   pgtype.Text{String: "linear", Valid: true},
			Visibility:   visibility.Private,
			EndpointSlug: "linear",
		},
		// Disabled attachment is skipped by per-server resolution and must not
		// cause a false mismatch against gateway members.
		{
			MCPServerID:  uuid.New(),
			ServerSlug:   pgtype.Text{String: "disabled-srv", Valid: true},
			Visibility:   visibility.Disabled,
			EndpointSlug: "disabled-srv",
		},
	}
	platformSlugs := []string{platformtools.AssistantsPlatformToolsetSlug}

	newCore := func(eval ProjectGatewayEvaluator) *ServiceCore {
		core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, testRuntimeBackend{backend: runtimeBackendGKE}, nil, nil, serverURL, telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, nil))
		core.SetProjectGatewayEvaluator(eval)
		return core
	}

	t.Run("exact match is eligible and re-evaluated live on every call", func(t *testing.T) {
		t.Parallel()
		stub := &stubProjectGatewayEvaluator{
			eval: func(context.Context, string, uuid.UUID, string, *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
				return orggateway.ProjectGatewayEvaluation{
					Admitted:   true,
					GatewayURL: gatewayURL,
					Members: []orggateway.ProjectGatewayMember{
						{ServerID: hostedID, EnvironmentSlug: "prod"},
						{ServerID: remoteID, EnvironmentSlug: ""},
						{ServerID: platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug), PlatformToolsetSlug: platformtools.AssistantsPlatformToolsetSlug},
					},
				}, nil
			},
		}
		core := newCore(stub)
		got := core.EvaluateProjectGatewayEligibility(t.Context(), orgID, projectID, assistantID, serverURL, "tok-1", attached, platformSlugs)
		require.True(t, got.Eligible)
		require.Equal(t, gatewayURL, got.GatewayURL)
		require.EqualValues(t, 1, stub.calls.Load())

		// Every call evaluates the live admitted-member set (no stale cache window).
		gotAgain := core.EvaluateProjectGatewayEligibility(t.Context(), orgID, projectID, assistantID, serverURL, "tok-1", attached, platformSlugs)
		require.True(t, gotAgain.Eligible)
		require.EqualValues(t, 2, stub.calls.Load())

		// Single gateway entry JSON carries is_gateway:true; per-server entries omit it.
		gwJSON, err := json.Marshal(projectGatewayRuntimeServer(got.GatewayURL))
		require.NoError(t, err)
		require.Contains(t, string(gwJSON), `"is_gateway":true`)

		perServer := resolveAssistantMCPServers(t.Context(), logger, serverURL, attached, platformSlugs)
		psJSON, err := json.Marshal(perServer)
		require.NoError(t, err)
		require.NotContains(t, string(psJSON), `"is_gateway"`)
	})

	t.Run("missing built-in grant falls back", func(t *testing.T) {
		t.Parallel()
		core := newCore(&stubProjectGatewayEvaluator{
			eval: func(context.Context, string, uuid.UUID, string, *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
				return orggateway.ProjectGatewayEvaluation{
					Admitted:   true,
					GatewayURL: gatewayURL,
					Members: []orggateway.ProjectGatewayMember{
						{ServerID: hostedID, EnvironmentSlug: "prod"},
						{ServerID: remoteID, EnvironmentSlug: ""},
					},
				}, nil
			},
		})
		got := core.EvaluateProjectGatewayEligibility(t.Context(), orgID, projectID, assistantID, serverURL, "tok-2", attached, platformSlugs)
		require.False(t, got.Eligible)
		require.Equal(t, "platform_toolset_mismatch", got.Reason)
	})

	t.Run("extra admitted server falls back", func(t *testing.T) {
		t.Parallel()
		core := newCore(&stubProjectGatewayEvaluator{
			eval: func(context.Context, string, uuid.UUID, string, *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
				return orggateway.ProjectGatewayEvaluation{
					Admitted:   true,
					GatewayURL: gatewayURL,
					Members: []orggateway.ProjectGatewayMember{
						{ServerID: hostedID, EnvironmentSlug: "prod"},
						{ServerID: remoteID, EnvironmentSlug: ""},
						{ServerID: extraID, EnvironmentSlug: ""},
						{ServerID: platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug), PlatformToolsetSlug: platformtools.AssistantsPlatformToolsetSlug},
					},
				}, nil
			},
		})
		got := core.EvaluateProjectGatewayEligibility(t.Context(), orgID, projectID, assistantID, serverURL, "tok-3", attached, platformSlugs)
		require.False(t, got.Eligible)
		require.Equal(t, "server_set_mismatch", got.Reason)
	})

	t.Run("missing attached server falls back", func(t *testing.T) {
		t.Parallel()
		core := newCore(&stubProjectGatewayEvaluator{
			eval: func(context.Context, string, uuid.UUID, string, *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
				return orggateway.ProjectGatewayEvaluation{
					Admitted:   true,
					GatewayURL: gatewayURL,
					Members: []orggateway.ProjectGatewayMember{
						{ServerID: hostedID, EnvironmentSlug: "prod"},
						{ServerID: platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug), PlatformToolsetSlug: platformtools.AssistantsPlatformToolsetSlug},
					},
				}, nil
			},
		})
		got := core.EvaluateProjectGatewayEligibility(t.Context(), orgID, projectID, assistantID, serverURL, "tok-4", attached, platformSlugs)
		require.False(t, got.Eligible)
		require.Equal(t, "server_set_mismatch", got.Reason)
	})

	t.Run("per-attachment environment override mismatch falls back", func(t *testing.T) {
		t.Parallel()
		overridden := append([]assistantMCPServerRow(nil), attached...)
		overridden[0].EnvironmentID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		overridden[0].EnvironmentSlug = pgtype.Text{String: "staging", Valid: true}

		core := newCore(&stubProjectGatewayEvaluator{
			eval: func(context.Context, string, uuid.UUID, string, *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
				return orggateway.ProjectGatewayEvaluation{
					Admitted:   true,
					GatewayURL: gatewayURL,
					Members: []orggateway.ProjectGatewayMember{
						{ServerID: hostedID, EnvironmentSlug: "prod"},
						{ServerID: remoteID, EnvironmentSlug: ""},
						{ServerID: platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug), PlatformToolsetSlug: platformtools.AssistantsPlatformToolsetSlug},
					},
				}, nil
			},
		})
		got := core.EvaluateProjectGatewayEligibility(t.Context(), orgID, projectID, assistantID, serverURL, "tok-5", overridden, platformSlugs)
		require.False(t, got.Eligible)
		require.Equal(t, "environment_override_mismatch", got.Reason)
	})

	t.Run("gateway admission rejection falls back with reason", func(t *testing.T) {
		t.Parallel()
		core := newCore(&stubProjectGatewayEvaluator{
			eval: func(context.Context, string, uuid.UUID, string, *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
				return orggateway.ProjectGatewayEvaluation{
					Admitted: false,
					Reason:   "machine_access_disabled",
				}, nil
			},
		})
		got := core.EvaluateProjectGatewayEligibility(t.Context(), orgID, projectID, assistantID, serverURL, "tok-6", attached, platformSlugs)
		require.False(t, got.Eligible)
		require.Equal(t, "machine_access_disabled", got.Reason)
	})
}

func TestDefaultRuntimeTurnAndBootstrap_UsesProjectGatewayAndFallsBack(t *testing.T) {
	t.Parallel()

	db, err := assistantsInfra.CloneTestDatabase(t, "default_runtime_project_gateway")
	require.NoError(t, err)
	ctx := t.Context()
	project := newProvisioningProject(t, db, "default-runtime-gw")
	seedProjectRead(t, db, "user-1", project)

	core := newProvisioningCore(t, db)
	assistant, err := core.CreateAssistant(ctx, "org-test", project, "user-1", "Gateway Assistant", "openai/gpt-4o-mini", "You are helpful.", nil, nil, 300, 1, StatusActive, true, nil, nil)
	require.NoError(t, err)
	thread := seedThreadWithEvent(t, db, assistant.ID, "gw-thread", "gw-thread", eventStatusPending)

	redisClient, err := assistantsInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{
		AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential,
		AdmitWorkloadSession:     runtimepolicy.AdmitWorkloadSession,
	})
	manager := assistanttokens.New("test-secret", db, engine, newTestPrincipalCredentials(t, db), cache.NewRedisCacheAdapter(redisClient))
	core.assistantTokens = manager

	runTurnMCP := &atomic.Pointer[[]runtimeMCPServer]{}
	runTurnToken := &atomic.Pointer[string]{}
	core.runtime = testRuntimeBackend{
		backend:           runtimeBackendGKE,
		runTurnMCPServers: runTurnMCP,
		runTurnToken:      runTurnToken,
	}

	gatewayURL := "https://api.example.test/mcp/org/acme/projects/default-runtime-gw"
	var eligible atomic.Bool
	eligible.Store(true)
	core.SetProjectGatewayEvaluator(&stubProjectGatewayEvaluator{
		eval: func(_ context.Context, _ string, _ uuid.UUID, _ string, _ *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
			if !eligible.Load() {
				return orggateway.ProjectGatewayEvaluation{Admitted: false, Reason: "machine_access_disabled"}, nil
			}
			return orggateway.ProjectGatewayEvaluation{
				Admitted:   true,
				GatewayURL: gatewayURL,
				Members: []orggateway.ProjectGatewayMember{{
					ServerID:            platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug),
					PlatformToolsetSlug: platformtools.AssistantsPlatformToolsetSlug,
				}},
			}, nil
		},
	})

	// 1. Eligible human-initiated turn carries exactly one gateway MCP entry.
	_, err = core.processEventTurn(ctx, assistantThreadRecord{
		ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: sourceKindDashboard, SourceRefJSON: []byte(`{"user_id":"user-1"}`),
	}, assistant, assistantRuntimeRecord{RuntimeVersion: 2}, assistantThreadEventRecord{
		ID: uuid.New(), EventID: uuid.NewString(), NormalizedPayloadJSON: []byte(`{"text":"hello","user_id":"user-1"}`),
	})
	require.NoError(t, err)
	gotMCP := runTurnMCP.Load()
	require.NotNil(t, gotMCP)
	require.Equal(t, []runtimeMCPServer{{
		ID:        projectGatewayRuntimeServerID,
		URL:       gatewayURL,
		Headers:   map[string]string{},
		IsGateway: true,
	}}, *gotMCP)

	// Bootstrap with the exact same turn token also returns the single gateway
	// entry and gateway-mode instructions.
	humanToken := *runTurnToken.Load()
	authedCtx, _, err := manager.AuthorizeRuntime(ctx, "Bearer "+humanToken)
	require.NoError(t, err)
	bootstrap, err := core.BuildThreadBootstrap(withBootstrapRuntimeToken(authedCtx, humanToken), project, thread, assistant.ID)
	require.NoError(t, err)
	require.Equal(t, *gotMCP, bootstrap.MCPServers)
	require.NotContains(t, bootstrap.Instructions, "assistant_mcp_auth_required")
	require.Contains(t, bootstrap.Instructions, "connection_status")

	// 2. Eligible no-human (cron/workload) turn also carries exactly one gateway entry.
	_, err = core.processEventTurn(ctx, assistantThreadRecord{
		ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: sourceKindCron, SourceRefJSON: []byte(`{}`),
	}, assistant, assistantRuntimeRecord{RuntimeVersion: 2}, assistantThreadEventRecord{
		ID: uuid.New(), EventID: uuid.NewString(), NormalizedPayloadJSON: []byte(`{"schedule":"* * * * *","fired_at":"2026-01-01T00:00:00Z"}`),
	})
	require.NoError(t, err)
	require.Equal(t, []runtimeMCPServer{{
		ID:        projectGatewayRuntimeServerID,
		URL:       gatewayURL,
		Headers:   map[string]string{},
		IsGateway: true,
	}}, *runTurnMCP.Load())

	// 2b. During a pending runtime switch (assistant desired RuntimeVersion=3
	// while the pinned active runtime row is still v2), bootstrap follows the
	// serving runtime row's version so it agrees with turn dispatch.
	queries := assistantrepo.New(db)
	require.NoError(t, queries.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
		AssistantThreadID: thread,
		AssistantID:       assistant.ID,
		ProjectID:         project,
		Backend:           runtimeBackendGKE,
		State:             "running",
	}))
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3,
		AssistantID:    assistant.ID,
		ProjectID:      project,
	}))
	pendingSwitchBootstrap, err := core.BuildThreadBootstrap(withBootstrapRuntimeToken(authedCtx, humanToken), project, thread, assistant.ID)
	require.NoError(t, err)
	require.Equal(t, *gotMCP, pendingSwitchBootstrap.MCPServers, "bootstrap must gate on active runtime row (v2), not desired runtime_version (v3)")
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 2,
		AssistantID:    assistant.ID,
		ProjectID:      project,
	}))

	// 3. When gateway becomes ineligible, both turn and bootstrap fall back to
	// the exact per-server list unchanged byte-for-byte.
	eligible.Store(false)
	_, err = core.processEventTurn(ctx, assistantThreadRecord{
		ID: thread, ProjectID: project, AssistantID: assistant.ID, SourceKind: sourceKindDashboard, SourceRefJSON: []byte(`{"user_id":"user-1"}`),
	}, assistant, assistantRuntimeRecord{RuntimeVersion: 2}, assistantThreadEventRecord{
		ID: uuid.New(), EventID: uuid.NewString(), NormalizedPayloadJSON: []byte(`{"text":"hello again","user_id":"user-1"}`),
	})
	require.NoError(t, err)
	wantFallback := resolveAssistantMCPServers(ctx, testenv.NewLogger(t), core.runtime.ServerURL(), assistant.MCPServers, []string{platformtools.AssistantsPlatformToolsetSlug})
	require.Equal(t, wantFallback, *runTurnMCP.Load())

	fallbackToken := *runTurnToken.Load()
	fallbackAuthed, _, err := manager.AuthorizeRuntime(ctx, "Bearer "+fallbackToken)
	require.NoError(t, err)
	fallbackBootstrap, err := core.BuildThreadBootstrap(withBootstrapRuntimeToken(fallbackAuthed, fallbackToken), project, thread, assistant.ID)
	require.NoError(t, err)
	require.Equal(t, wantFallback, fallbackBootstrap.MCPServers)
	require.Contains(t, fallbackBootstrap.Instructions, "assistant_mcp_auth_required")
}

func TestDefaultRuntime_AssistantSlackBuiltInVsAttachedSlackToolset(t *testing.T) {
	t.Parallel()

	db, err := assistantsInfra.CloneTestDatabase(t, "default_runtime_assistant_slack_gw")
	require.NoError(t, err)
	ctx := t.Context()
	project := newProvisioningProject(t, db, "slack-gw-proj")
	seedProjectRead(t, db, "user-1", project)

	core := newProvisioningCore(t, db)
	redisClient, err := assistantsInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	engine := authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, nil, authz.EngineOpts{
		AdmitPrincipalCredential: runtimepolicy.AdmitPrincipalCredential,
		AdmitWorkloadSession:     runtimepolicy.AdmitWorkloadSession,
	})
	manager := assistanttokens.New("test-secret", db, engine, newTestPrincipalCredentials(t, db), cache.NewRedisCacheAdapter(redisClient))
	core.assistantTokens = manager

	runTurnMCP := &atomic.Pointer[[]runtimeMCPServer]{}
	runTurnPrompt := &atomic.Pointer[string]{}
	runTurnToken := &atomic.Pointer[string]{}
	core.runtime = testRuntimeBackend{
		backend:           runtimeBackendGKE,
		runTurnMCPServers: runTurnMCP,
		runTurnPrompt:     runTurnPrompt,
		runTurnToken:      runTurnToken,
	}

	gatewayURL := "https://api.example.test/mcp/org/acme/projects/slack-gw-proj"

	// 1. Eligible assistant with built-in assistant-slack:
	//    - agent holds mcp:connect on PlatformToolsetID("assistant-slack"), so
	//      assistantPlatformSlugs includes "assistant-slack"
	//    - gateway admits ["assistants", "assistant-slack"]
	//    - both turn dispatch and bootstrap use the single gateway entry and
	//      Slack guidance + turn decode name assistant-slack--<tool>.
	builtInAssistant, err := core.CreateAssistant(ctx, "org-test", project, "user-1", "Built-in Slack Assistant", "openai/gpt-4o-mini", "You are helpful.", nil, nil, 300, 1, StatusActive, true, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, builtInAssistant.AgentID)

	assistantSlackID := platformtools.PlatformToolsetID(platformtools.AssistantSlackPlatformToolsetSlug)
	selBytes, err := assistantidentity.BuiltinConnectGrant(project, platformtools.AssistantSlackPlatformToolsetSlug).Selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: "org-test",
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeAgent, *builtInAssistant.AgentID),
		Scope:          string(authz.ScopeMCPConnect),
		Selectors:      selBytes,
	})
	require.NoError(t, err)

	builtInThread := seedThreadWithEvent(t, db, builtInAssistant.ID, "slack-builtin", "slack:T1:C1:1.0", eventStatusPending)
	core.SetProjectGatewayEvaluator(&stubProjectGatewayEvaluator{
		eval: func(_ context.Context, _ string, _ uuid.UUID, _ string, _ *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
			return orggateway.ProjectGatewayEvaluation{
				Admitted:   true,
				GatewayURL: gatewayURL,
				Members: []orggateway.ProjectGatewayMember{
					{
						ServerID:            platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug),
						PlatformToolsetSlug: platformtools.AssistantsPlatformToolsetSlug,
					},
					{
						ServerID:            assistantSlackID,
						PlatformToolsetSlug: platformtools.AssistantSlackPlatformToolsetSlug,
					},
				},
			}, nil
		},
	})

	slackPayload := []byte(`{"event_type":"message","addressed":true,"team_id":"T1","channel_id":"C1","thread_id":"1.0","user_id":"U1","text":"hi","thread_context":{"unavailable":true}}`)
	_, err = core.processEventTurn(ctx, assistantThreadRecord{
		ID: builtInThread, ProjectID: project, AssistantID: builtInAssistant.ID, SourceKind: sourceKindSlack, SourceRefJSON: []byte(`{"team_id":"T1","channel_id":"C1","thread_id":"1.0","user_id":"U1"}`),
	}, builtInAssistant, assistantRuntimeRecord{RuntimeVersion: 2}, assistantThreadEventRecord{
		ID: uuid.New(), EventID: uuid.NewString(), NormalizedPayloadJSON: slackPayload,
	})
	require.NoError(t, err)
	wantGateway := []runtimeMCPServer{{
		ID:        projectGatewayRuntimeServerID,
		URL:       gatewayURL,
		Headers:   map[string]string{},
		IsGateway: true,
	}}
	require.Equal(t, wantGateway, *runTurnMCP.Load())
	require.Contains(t, *runTurnPrompt.Load(), "execute_read_only_tool with assistant-slack--platform_slack_read_thread_messages")

	builtInTok := *runTurnToken.Load()
	builtInAuthed, _, err := manager.AuthorizeRuntime(ctx, "Bearer "+builtInTok)
	require.NoError(t, err)
	builtInBootstrap, err := core.BuildThreadBootstrap(withBootstrapRuntimeToken(builtInAuthed, builtInTok), project, builtInThread, builtInAssistant.ID)
	require.NoError(t, err)
	require.Equal(t, wantGateway, builtInBootstrap.MCPServers)
	require.Contains(t, builtInBootstrap.Instructions, "execute_tool with assistant-slack--platform_slack_send_message")
	require.Contains(t, builtInBootstrap.Instructions, "execute_tool with assistant-slack--platform_slack_post_ephemeral")
	require.NotContains(t, builtInBootstrap.Instructions, "speakeasy-slack--")
	require.NotContains(t, builtInBootstrap.Instructions, "assistant_mcp_auth_required")

	// 2. Assistant with an attached Slack toolset (no assistant-slack grant,
	//    attached Slack MCP server not admitted on the gateway): both turn
	//    dispatch and bootstrap stay in per-server mode with per-server guidance.
	attachedToolset, err := toolsetsrepo.New(db).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test",
		ProjectID:      project,
		Name:           "Attached Assistant Slack",
		Slug:           "attached-slack-bot",
		Description:    conv.ToPGText("Per-assistant Slack toolset"),
	})
	require.NoError(t, err)
	createCanonicalWrapper(t, db, attachedToolset, "attached-slack-bot", visibility.Private)
	attachedAssistant, err := core.CreateAssistant(ctx, "org-test", project, "user-1", "Attached Slack Assistant", "openai/gpt-4o-mini", "You are helpful.", nil, []*types.AssistantMCPServerRef{{McpServerSlug: "attached-slack-bot"}}, 300, 1, StatusActive, true, func(uuid.UUID) bool { return true }, nil)
	require.NoError(t, err)
	attachedThread := seedThreadWithEvent(t, db, attachedAssistant.ID, "slack-attached", "slack:T1:C2:2.0", eventStatusPending)

	// Gateway admits only "assistants" (not the attached Slack server).
	core.SetProjectGatewayEvaluator(&stubProjectGatewayEvaluator{
		eval: func(_ context.Context, _ string, _ uuid.UUID, _ string, _ *url.URL) (orggateway.ProjectGatewayEvaluation, error) {
			return orggateway.ProjectGatewayEvaluation{
				Admitted:   true,
				GatewayURL: gatewayURL,
				Members: []orggateway.ProjectGatewayMember{{
					ServerID:            platformtools.PlatformToolsetID(platformtools.AssistantsPlatformToolsetSlug),
					PlatformToolsetSlug: platformtools.AssistantsPlatformToolsetSlug,
				}},
			}, nil
		},
	})

	_, err = core.processEventTurn(ctx, assistantThreadRecord{
		ID: attachedThread, ProjectID: project, AssistantID: attachedAssistant.ID, SourceKind: sourceKindSlack, SourceRefJSON: []byte(`{"team_id":"T1","channel_id":"C2","thread_id":"2.0","user_id":"U1"}`),
	}, attachedAssistant, assistantRuntimeRecord{RuntimeVersion: 2}, assistantThreadEventRecord{
		ID: uuid.New(), EventID: uuid.NewString(), NormalizedPayloadJSON: slackPayload,
	})
	require.NoError(t, err)
	wantAttachedPerServer := resolveAssistantMCPServers(ctx, testenv.NewLogger(t), core.runtime.ServerURL(), attachedAssistant.MCPServers, []string{platformtools.AssistantsPlatformToolsetSlug})
	require.Equal(t, wantAttachedPerServer, *runTurnMCP.Load())
	require.Contains(t, *runTurnPrompt.Load(), "read them with platform_slack_read_thread_messages")
	require.NotContains(t, *runTurnPrompt.Load(), "assistant-slack--")

	attachedTok := *runTurnToken.Load()
	attachedAuthed, _, err := manager.AuthorizeRuntime(ctx, "Bearer "+attachedTok)
	require.NoError(t, err)
	attachedBootstrap, err := core.BuildThreadBootstrap(withBootstrapRuntimeToken(attachedAuthed, attachedTok), project, attachedThread, attachedAssistant.ID)
	require.NoError(t, err)
	require.Equal(t, wantAttachedPerServer, attachedBootstrap.MCPServers)
	require.Contains(t, attachedBootstrap.Instructions, "assistant_mcp_auth_required")
	require.NotContains(t, attachedBootstrap.Instructions, "assistant-slack--")
}
