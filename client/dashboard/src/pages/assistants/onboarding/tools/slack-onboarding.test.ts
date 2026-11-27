// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import {
  ASSISTANT_SLACK_TOOLSET_ID,
  SLACK_APP_AUTH_CLIENT_SCOPE,
} from "@/lib/auth-clients";
import { buildAssistantTools, type ToolDeps } from "./useOnboardingTools";

vi.mock("@/contexts/Sdk", () => ({ useSdkClient: vi.fn() }));
vi.mock("@/elements", () => ({
  defineFrontendTool: (definition: unknown) => definition,
}));

type ExecutableTool = {
  execute: (
    args: Record<string, unknown>,
    ctx: { toolCallId: string },
  ) => Promise<Record<string, unknown>>;
};

function asExecutable(tool: unknown): ExecutableTool {
  return tool as ExecutableTool;
}

function makeDeps(overrides?: {
  canManageAuthClients?: boolean;
  existingHeaders?: {
    name: string;
    isSecret: boolean;
    hasStoredValue: boolean;
  }[];
  pendingSubmit?: (toolCallId: string, resolve: (v: unknown) => void) => void;
}) {
  const assistant = {
    id: "asst-1",
    name: "Mina",
    agentId: "agent-1",
    instructions: "# Personality\nFriendly.",
    toolsets: [] as { toolsetSlug: string }[],
  };
  const authClientsCreate = vi.fn().mockResolvedValue({
    id: "client-slack-1",
    clientType: "slack_app",
    name: "Mina Slack",
    headers: overrides?.existingHeaders ?? [],
  });
  const authClientsGet = vi.fn().mockResolvedValue({
    id: "client-slack-1",
    clientType: "slack_app",
    name: "Mina Slack",
    headers: overrides?.existingHeaders ?? [],
  });
  const authClientsList = vi.fn().mockResolvedValue({
    async *[Symbol.asyncIterator]() {
      yield { result: { items: [] } };
    },
  });
  const authClientsSetHeaders = vi.fn().mockResolvedValue({
    id: "client-slack-1",
    clientType: "slack_app",
    name: "Mina Slack",
    headers: [],
  });
  const agentsListPolicyGrants = vi.fn().mockResolvedValue([]);
  const agentsCreatePolicyGrant = vi.fn().mockResolvedValue({ id: "grant-1" });
  const triggersList = vi.fn().mockResolvedValue({ triggers: [] });
  const triggersCreate = vi.fn().mockResolvedValue({
    id: "trig-slack-1",
    name: "Slack",
    definitionSlug: "slack",
    webhookUrl: "https://api.example.invalid/rpc/triggers/trig-slack-1/webhook",
  });
  const toolsetsCreate = vi.fn();
  const toolsetsUpdateBySlug = vi.fn();
  const environmentsCreate = vi.fn();
  const environmentsUpdateBySlug = vi.fn();
  const assistantsUpdate = vi
    .fn()
    .mockImplementation(async ({ updateAssistantForm }) => ({
      ...assistant,
      instructions: updateAssistantForm.instructions ?? assistant.instructions,
    }));

  const sdk = {
    assistants: {
      update: assistantsUpdate,
      list: vi.fn().mockResolvedValue({ assistants: [assistant] }),
    },
    authClients: {
      create: authClientsCreate,
      get: authClientsGet,
      list: authClientsList,
      setHeaders: authClientsSetHeaders,
    },
    agents: {
      listPolicyGrants: agentsListPolicyGrants,
      createPolicyGrant: agentsCreatePolicyGrant,
    },
    triggers: {
      list: triggersList,
      create: triggersCreate,
      update: vi.fn(),
      delete: vi.fn(),
    },
    toolsets: {
      list: vi.fn().mockResolvedValue({ toolsets: [] }),
      create: toolsetsCreate,
      updateBySlug: toolsetsUpdateBySlug,
    },
    environments: {
      list: vi.fn().mockResolvedValue({ environments: [] }),
      create: environmentsCreate,
      updateBySlug: environmentsUpdateBySlug,
    },
  } as unknown as ToolDeps["sdk"];

  const draft = {
    assistantId: assistant.id,
    assistant,
    assistantEnv: null,
    setAssistant: vi.fn(),
    setAssistantEnv: vi.fn(),
    invalidateAll: vi.fn(),
    getReasoningEffort: vi.fn().mockReturnValue(undefined),
    registerPending: vi.fn(
      (toolCallId: string, resolve: (v: unknown) => void) => {
        overrides?.pendingSubmit?.(toolCallId, resolve);
      },
    ),
  } as unknown as ToolDeps["draft"];

  return {
    deps: {
      sdk,
      organizationId: "org-1",
      projectId: "proj-1",
      canManageAuthClients: overrides?.canManageAuthClients ?? true,
      draft,
    } satisfies ToolDeps,
    spies: {
      authClientsCreate,
      authClientsGet,
      authClientsSetHeaders,
      agentsCreatePolicyGrant,
      triggersList,
      triggersCreate,
      toolsetsCreate,
      toolsetsUpdateBySlug,
      environmentsCreate,
      environmentsUpdateBySlug,
      registerPending: draft.registerPending,
    },
  };
}

describe("Slack onboarding tools", () => {
  it("creates a slack_app auth client, built-in grants, and auth-client trigger without creating a toolset or touching environments", async () => {
    const { deps, spies } = makeDeps({
      pendingSubmit: (_id, resolve) =>
        resolve({
          success: true,
          capabilities: ["send", "read"],
          events: [],
          reply_mode: "threads",
        }),
    });
    const tools = buildAssistantTools(deps);
    const result = await asExecutable(tools.propose_slack_setup).execute(
      { preselected_capabilities: ["send"] },
      { toolCallId: "tc-1" },
    );

    expect(result.ok).toBe(true);
    expect(result.auth_client_id).toBe("client-slack-1");
    expect(result.trigger_id).toBe("trig-slack-1");
    expect(result.webhook_url).toBe(
      "https://api.example.invalid/rpc/triggers/trig-slack-1/webhook",
    );

    expect(spies.authClientsCreate).toHaveBeenCalledWith({
      createAuthClientRequestBody: {
        clientType: "slack_app",
        name: "Mina Slack",
        projectId: "proj-1",
      },
    });
    expect(spies.agentsCreatePolicyGrant).toHaveBeenCalledWith({
      createAgentPolicyGrantForm: {
        agentId: "agent-1",
        effect: "allow",
        scope: "mcp:connect",
        selector: {
          resourceKind: "mcp",
          resourceId: ASSISTANT_SLACK_TOOLSET_ID,
          projectId: "proj-1",
        },
      },
    });
    expect(spies.agentsCreatePolicyGrant).toHaveBeenCalledWith({
      createAgentPolicyGrantForm: {
        agentId: "agent-1",
        effect: "allow",
        scope: "mcp:auth",
        selector: {
          resourceKind: "mcp_auth_client",
          resourceId: "client-slack-1",
        },
      },
    });
    expect(spies.triggersCreate).toHaveBeenCalledWith({
      createTriggerInstanceForm: expect.objectContaining({
        definitionSlug: "slack",
        authClientId: "client-slack-1",
      }),
    });
    expect(
      spies.triggersCreate.mock.calls[0]?.[0]?.createTriggerInstanceForm
        ?.environmentId,
    ).toBeUndefined();
    expect(spies.toolsetsCreate).not.toHaveBeenCalled();
    expect(spies.toolsetsUpdateBySlug).not.toHaveBeenCalled();
    expect(spies.environmentsCreate).not.toHaveBeenCalled();
    expect(spies.environmentsUpdateBySlug).not.toHaveBeenCalled();
  });

  it("saves Slack credentials onto the slack_app auth client while preserving existing secrets and never touching environments", async () => {
    const { deps, spies } = makeDeps({
      existingHeaders: [
        { name: "SLACK_SIGNING_SECRET", isSecret: true, hasStoredValue: true },
      ],
      pendingSubmit: (_id, resolve) =>
        resolve({
          success: true,
          values: {
            SLACK_BOT_TOKEN: "xoxb-new-token",
            SLACK_USER_TOKEN: "xoxp-new-token",
          },
        }),
    });
    spies.triggersList.mockResolvedValueOnce({
      triggers: [
        {
          id: "trig-slack-1",
          definitionSlug: "slack",
          targetKind: "assistant",
          targetRef: "asst-1",
          authClientId: "client-slack-1",
        },
      ],
    });
    const tools = buildAssistantTools(deps);
    const result = await asExecutable(tools.request_slack_credentials).execute(
      {
        auth_client_id: "client-slack-1",
        keys: [
          { name: "SLACK_BOT_TOKEN", sensitive: true },
          { name: "SLACK_USER_TOKEN", sensitive: true },
        ],
      },
      { toolCallId: "tc-2" },
    );

    expect(result.ok).toBe(true);
    expect(result.saved).toBe(true);
    expect(spies.authClientsSetHeaders).toHaveBeenCalledWith({
      setAuthClientHeadersRequestBody: {
        id: "client-slack-1",
        headers: [
          { name: "SLACK_SIGNING_SECRET", isSecret: true },
          { name: "SLACK_BOT_TOKEN", isSecret: true, value: "xoxb-new-token" },
          { name: "SLACK_USER_TOKEN", isSecret: true, value: "xoxp-new-token" },
        ],
      },
    });
    expect(spies.environmentsCreate).not.toHaveBeenCalled();
    expect(spies.environmentsUpdateBySlug).not.toHaveBeenCalled();
  });

  it("rejects an explicit auth_client_id that does not belong to the assistant and propagates authClients.get errors", async () => {
    const { deps, spies } = makeDeps({
      pendingSubmit: (_id, resolve) =>
        resolve({
          success: true,
          values: { SLACK_BOT_TOKEN: "xoxb-new-token" },
        }),
    });
    spies.triggersList.mockResolvedValue({
      triggers: [
        {
          id: "trig-slack-1",
          definitionSlug: "slack",
          targetKind: "assistant",
          targetRef: "asst-1",
          authClientId: "client-slack-1",
        },
      ],
    });
    const tools = buildAssistantTools(deps);

    const mismatch = await asExecutable(
      tools.request_slack_credentials,
    ).execute(
      {
        auth_client_id: "client-other-assistant",
        keys: [{ name: "SLACK_BOT_TOKEN", sensitive: true }],
      },
      { toolCallId: "tc-mismatch" },
    );
    expect(mismatch.ok).toBe(false);
    expect(String(mismatch.error)).toContain("does not belong");
    expect(spies.authClientsSetHeaders).not.toHaveBeenCalled();

    spies.authClientsGet
      .mockResolvedValueOnce({
        id: "client-slack-1",
        clientType: "slack_app",
        name: "Mina Slack",
        headers: [],
      })
      .mockRejectedValueOnce(new Error("transient read failure"));
    const readFail = await asExecutable(
      tools.request_slack_credentials,
    ).execute(
      {
        auth_client_id: "client-slack-1",
        keys: [{ name: "SLACK_BOT_TOKEN", sensitive: true }],
      },
      { toolCallId: "tc-readfail" },
    );
    expect(readFail.ok).toBe(false);
    expect(String(readFail.error)).toContain("transient read failure");
    expect(spies.authClientsSetHeaders).not.toHaveBeenCalled();
  });

  it("returns a clear upfront permission message without rendering a card when caller lacks org:admin", async () => {
    const { deps, spies } = makeDeps({ canManageAuthClients: false });
    const tools = buildAssistantTools(deps);

    const setupResult = await asExecutable(tools.propose_slack_setup).execute(
      {},
      { toolCallId: "tc-3" },
    );
    expect(setupResult.ok).toBe(false);
    expect(setupResult.required_scope).toBe(SLACK_APP_AUTH_CLIENT_SCOPE);
    expect(String(setupResult.error)).toContain("organization administrator");

    const credsResult = await asExecutable(
      tools.request_slack_credentials,
    ).execute(
      {
        auth_client_id: "client-slack-1",
        keys: [{ name: "SLACK_BOT_TOKEN", sensitive: true }],
      },
      { toolCallId: "tc-4" },
    );
    expect(credsResult.ok).toBe(false);
    expect(credsResult.required_scope).toBe(SLACK_APP_AUTH_CLIENT_SCOPE);
    expect(spies.registerPending).not.toHaveBeenCalled();
    expect(spies.authClientsCreate).not.toHaveBeenCalled();
    expect(spies.authClientsSetHeaders).not.toHaveBeenCalled();
  });

  it("refuses saving Slack credentials through request_environment_secrets", async () => {
    const { deps, spies } = makeDeps();
    const tools = buildAssistantTools(deps);

    const result = await asExecutable(
      tools.request_environment_secrets,
    ).execute(
      {
        keys: [{ name: "SLACK_BOT_TOKEN", sensitive: true }],
      },
      { toolCallId: "tc-5" },
    );

    expect(result.ok).toBe(false);
    expect(String(result.error)).toContain("request_slack_credentials");
    expect(spies.environmentsCreate).not.toHaveBeenCalled();
    expect(spies.environmentsUpdateBySlug).not.toHaveBeenCalled();
  });
});
