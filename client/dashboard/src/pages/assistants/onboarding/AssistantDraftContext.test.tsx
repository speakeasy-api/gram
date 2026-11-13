import { act, cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { AssistantDraftProvider } from "./AssistantDraftContext";
import { useAssistantDraft } from "./useAssistantDraft";

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project", slug: "project" }),
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
afterEach(cleanup);

it("initializes a real new-assistant provider with Medium reasoning effort", () => {
  const queryClient = new QueryClient();
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AssistantDraftProvider initialAssistantId={null}>
          {children}
        </AssistantDraftProvider>
      </QueryClientProvider>
    );
  }
  const { result } = renderHook(() => useAssistantDraft(), {
    wrapper: Wrapper,
  });
  expect(result.current.assistantId).toBeNull();
  expect(result.current.reasoningEffort).toBe("medium");
  act(() => result.current.setReasoningEffort("none"));
  expect(result.current.reasoningEffort).toBe("none");
});
