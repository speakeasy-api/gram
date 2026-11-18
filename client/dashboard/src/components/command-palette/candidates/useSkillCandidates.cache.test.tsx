import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ requests: 0 }));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-one",
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project-one" }),
}));
vi.mock("@gram/client/react-query/skills.js", async () => {
  const { useInfiniteQuery } = await import("@tanstack/react-query");
  return {
    useSkillsInfinite: (
      request: unknown,
      _security: unknown,
      options: Record<string, unknown>,
    ) =>
      useInfiniteQuery({
        queryKey: ["skills", request],
        queryFn: async () => {
          state.requests += 1;
          return {
            result: {
              skills: [
                {
                  id: "skill-one",
                  displayName: "Skill One",
                  name: "skill-one",
                  classification: "verified",
                  tags: [],
                },
              ],
            },
          };
        },
        initialPageParam: undefined,
        getNextPageParam: () => undefined,
        ...options,
      }),
  };
});

import { useSkillCandidates } from "./useSkillCandidates";

it("reuses the fully loaded skill inventory on a quick reopen", async () => {
  state.requests = 0;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
  const { result, rerender, unmount } = renderHook(
    ({ enabled }) => useSkillCandidates({ enabled }),
    { initialProps: { enabled: true }, wrapper },
  );

  try {
    await waitFor(() => expect(result.current).toHaveLength(1));
    expect(state.requests).toBe(1);

    rerender({ enabled: false });
    rerender({ enabled: true });
    await Promise.resolve();

    expect(result.current).toHaveLength(1);
    expect(state.requests).toBe(1);
  } finally {
    unmount();
    client.clear();
  }
});
