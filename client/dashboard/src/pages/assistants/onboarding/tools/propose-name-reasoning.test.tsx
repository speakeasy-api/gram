import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AssistantDraftProvider } from "../AssistantDraftContext";
import { useAssistantDraft } from "../useAssistantDraft";
import { useOnboardingTools } from "./useOnboardingTools";

const mocks = vi.hoisted(() => ({ create: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project", slug: "project" }),
  useSession: () => ({ activeOrganizationId: "org" }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ assistants: { create: mocks.create } }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/elements", () => ({
  defineFrontendTool: (definition: unknown) => definition,
}));
vi.mock(
  "@gram/client/react-query/assistantsGet.js",
  async (importOriginal) => ({
    ...(await importOriginal<
      typeof import("@gram/client/react-query/assistantsGet.js")
    >()),
    useAssistantsGet: () => ({ data: undefined, refetch: vi.fn() }),
  }),
);
beforeEach(() => {
  mocks.create.mockReset();
  mocks.create.mockResolvedValue({
    id: "assistant",
    name: "Example",
    model: "example/model",
    instructions: "",
    toolsets: [],
  });
});
afterEach(cleanup);

it.each(["high", "none", "medium"] as const)(
  "creates with current %s reasoning after a pending name confirmation",
  async (effort) => {
    const client = new QueryClient();
    function Wrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={client}>
          <AssistantDraftProvider initialAssistantId={null}>
            {children}
          </AssistantDraftProvider>
        </QueryClientProvider>
      );
    }
    const { result } = renderHook(
      () => ({ draft: useAssistantDraft(), tools: useOnboardingTools() }),
      { wrapper: Wrapper },
    );
    expect(result.current.draft.reasoningEffort).toBe("medium");
    const initialDraft = result.current.draft;
    const tool = result.current.tools.frontendTools.propose_name as unknown as {
      execute: (
        args: Record<string, unknown>,
        ctx: { toolCallId: string },
      ) => Promise<unknown>;
    };
    // Start the original tool closure and leave it waiting on the name picker.
    const pending = tool.execute(
      { name_suggestions: ["Alex", "Sam", "Morgan"] },
      { toolCallId: "name-picker" },
    );
    expect(mocks.create).not.toHaveBeenCalled();
    if (effort !== "medium") {
      act(() => result.current.draft.setReasoningEffort(effort));
      expect(result.current.draft.reasoningEffort).toBe(effort);
      expect(initialDraft.reasoningEffort).toBe("medium");
    }
    await act(async () => {
      expect(
        result.current.draft.resolvePending("name-picker", {
          success: true,
          name: "Example",
        }),
      ).toBe(true);
      expect(await pending).toEqual(expect.objectContaining({ ok: true }));
    });
    expect(mocks.create).toHaveBeenCalledExactlyOnceWith({
      createAssistantForm: expect.objectContaining({ reasoningEffort: effort }),
    });
  },
);
