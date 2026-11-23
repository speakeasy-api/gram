import { renderHook, cleanup } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useOnboardingTools } from "./useOnboardingTools";
const mocks = vi.hoisted(() => ({
  effort: "medium" as "none" | "low" | "medium" | "high",
  create: vi.fn(),
  listEnvironments: vi.fn(),
  createEnvironment: vi.fn(),
  setAssistantEnv: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project" }),
  useSession: () => ({ activeOrganizationId: "org" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    assistants: { create: mocks.create },
    environments: {
      list: mocks.listEnvironments,
      create: mocks.createEnvironment,
    },
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("../useAssistantDraft", () => ({
  useAssistantDraft: () => ({
    assistantId: null,
    reasoningEffort: mocks.effort,
    getReasoningEffort: () => mocks.effort,
    setAssistant: vi.fn(),
    invalidateAll: vi.fn(),
    assistantEnv: null,
    setAssistantEnv: mocks.setAssistantEnv,
  }),
}));
vi.mock("@/elements", () => ({
  defineFrontendTool: (definition: unknown) => definition,
}));
beforeEach(() => {
  vi.clearAllMocks();
  mocks.listEnvironments.mockResolvedValue({ environments: [] });
  mocks.createEnvironment.mockResolvedValue({
    id: "env",
    slug: "env",
    name: "Example env",
    entries: [],
  });
});
afterEach(cleanup);
describe("onboarding reasoning creation payload", () => {
  it.each(["medium", "none", "low", "high"] as const)(
    "creates with selected %s effort",
    async (effort) => {
      mocks.effort = effort;
      mocks.create.mockResolvedValue({
        id: "assistant",
        name: "Example",
        model: "example/model",
        instructions: "",
      });
      const { result } = renderHook(() => useOnboardingTools());
      const tool = result.current.frontendTools.update_assistant as unknown as {
        execute: (args: Record<string, unknown>) => Promise<unknown>;
      };
      const response = await tool.execute({ name: "Example" });
      expect(response).toEqual(
        expect.objectContaining({
          ok: true,
          environment: { id: "env", slug: "env" },
        }),
      );
      expect(mocks.listEnvironments).toHaveBeenCalledExactlyOnceWith();
      expect(mocks.createEnvironment).toHaveBeenCalledExactlyOnceWith({
        createEnvironmentForm: {
          name: "Example env",
          description: "Shared credentials for Example.",
          entries: [],
          organizationId: "org",
        },
      });
      expect(mocks.setAssistantEnv).toHaveBeenCalledExactlyOnceWith({
        id: "env",
        slug: "env",
      });
      expect(mocks.create).toHaveBeenLastCalledWith({
        createAssistantForm: expect.objectContaining({
          reasoningEffort: effort,
        }),
      });
    },
  );
});
