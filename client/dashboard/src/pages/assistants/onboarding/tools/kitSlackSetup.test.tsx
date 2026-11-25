import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useOnboardingTools } from "./useOnboardingTools";

const mocks = vi.hoisted(() => {
  const assistant = {
    id: "assistant",
    agentId: "agent",
    name: "Casey",
    runtimeVersion: 3,
    toolsets: [],
  };
  const environment = { id: "env", slug: "casey-env" };
  return {
    assistant,
    environment,
    updateAssistant: vi.fn().mockResolvedValue(assistant),
    createTrigger: vi.fn().mockResolvedValue({
      id: "trigger",
      webhookUrl: "https://example.test/slack",
    }),
    provision: vi.fn().mockResolvedValue("casey-slack"),
    registerPending: vi.fn(),
    invalidateAll: vi.fn(),
  };
});

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project" }),
  useSession: () => ({ activeOrganizationId: "org" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    assistants: { update: mocks.updateAssistant },
    environments: { list: async () => ({ environments: [mocks.environment] }) },
    toolsets: { list: async () => ({ toolsets: [] }) },
    triggers: {
      list: async () => ({ triggers: [] }),
      create: mocks.createTrigger,
    },
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("../useAssistantDraft", () => ({
  useAssistantDraft: () => ({
    assistantId: mocks.assistant.id,
    assistant: mocks.assistant,
    assistantEnv: mocks.environment,
    setAssistant: vi.fn(),
    setAssistantEnv: vi.fn(),
    invalidateAll: mocks.invalidateAll,
    registerPending: mocks.registerPending,
  }),
}));
vi.mock("@/elements", () => ({
  defineFrontendTool: (definition: unknown) => definition,
}));
vi.mock("./kitSlackAccess", () => ({
  provisionKitSlackAccess: mocks.provision,
}));
afterEach(cleanup);

it("runs the approved Slack setup through Kit access and returns a webhook for installation", async () => {
  mocks.registerPending.mockImplementationOnce(
    (_id, finish: (result: unknown) => void) => {
      finish({ success: true, capabilities: ["send"], reply_mode: "mentions" });
    },
  );
  const { result } = renderHook(() => useOnboardingTools());
  const tool = result.current.frontendTools.propose_slack_setup as unknown as {
    execute: (
      args: Record<string, unknown>,
      ctx: { toolCallId: string },
    ) => Promise<unknown>;
  };
  expect(await tool.execute({}, { toolCallId: "setup" })).toEqual(
    expect.objectContaining({
      ok: true,
      mcp_server_slug: "casey-slack",
      trigger_id: "trigger",
      webhook_url: "https://example.test/slack",
      environment_slug: "casey-env",
      note: expect.stringContaining("credentials"),
    }),
  );
  expect(mocks.provision).toHaveBeenCalledExactlyOnceWith(
    expect.anything(),
    expect.objectContaining({
      agentId: "agent",
      environment: mocks.environment,
      toolUrns: expect.arrayContaining(["tools:platform:slack:send_message"]),
    }),
  );
  expect(mocks.createTrigger).toHaveBeenCalledExactlyOnceWith({
    createTriggerInstanceForm: expect.objectContaining({
      definitionSlug: "slack",
      targetRef: "assistant",
      environmentId: "env",
    }),
  });
  expect(mocks.updateAssistant).toHaveBeenCalledExactlyOnceWith({
    updateAssistantForm: { id: "assistant" },
  });
  expect(mocks.invalidateAll).toHaveBeenLastCalledWith({ kitAgentId: "agent" });
});
