import { renderHook, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useOnboardingTools } from "./useOnboardingTools";
const mocks = vi.hoisted(() => ({
  effort: "medium" as "none" | "low" | "medium" | "high",
  create: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project" }),
  useSession: () => ({ activeOrganizationId: "org" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    assistants: { create: mocks.create },
    environments: { create: async () => ({ id: "env", slug: "env" }) },
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("../useAssistantDraft", () => ({
  useAssistantDraft: () => ({
    assistantId: null,
    reasoningEffort: mocks.effort,
    setAssistant: vi.fn(),
    invalidateAll: vi.fn(),
    assistantEnv: null,
    setAssistantEnv: vi.fn(),
  }),
}));
vi.mock("@/elements", () => ({
  defineFrontendTool: (definition: unknown) => definition,
}));
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
      await tool.execute({ name: "Example" });
      expect(mocks.create).toHaveBeenLastCalledWith({
        createAssistantForm: expect.objectContaining({
          reasoningEffort: effort,
        }),
      });
    },
  );
});
