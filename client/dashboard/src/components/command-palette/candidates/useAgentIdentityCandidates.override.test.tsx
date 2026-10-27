import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import { saveState } from "@/pages/platform-admin/rbac-override-state";
import { useAgentIdentityCandidates } from "./useAgentIdentityCandidates";

const mocks = vi.hoisted(() => ({ fetchAgents: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => true,
  useOrganization: () => ({ id: "org-example", slug: "example" }),
  useSession: () => ({ user: { id: "user-example" } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
  useProjectSlugForRequests: () => "project-example",
  useSlugs: () => ({ projectSlug: "project-example" }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    identities: {
      detail: {
        overview: { href: () => "/overview" },
        permissions: { href: () => "/permissions" },
      },
    },
  }),
}));
vi.mock("@/pages/identities/identityRoster", () => ({
  fetchRegisteredAgents: (...args: unknown[]) => mocks.fetchAgents(...args),
}));

afterEach(() => {
  cleanup();
  localStorage.clear();
});

it("never reuses unrestricted action candidates while loading an override inventory", async () => {
  localStorage.clear();
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const page = (write: boolean) => ({
    items: [
      { id: "agent-example", name: "Build agent", permissions: { write } },
    ],
  });
  const narrowed = Promise.withResolvers<ReturnType<typeof page>>();
  mocks.fetchAgents
    .mockResolvedValueOnce(page(true))
    .mockReturnValueOnce(narrowed.promise);
  const { result } = renderHook(
    () => useAgentIdentityCandidates({ enabled: true }),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>
          <MemoryRouter>{children}</MemoryRouter>
        </QueryClientProvider>
      ),
    },
  );
  await waitFor(() => expect(result.current).toHaveLength(2));

  act(() => {
    saveState({
      enabled: true,
      scopes: { "agent:read": { enabled: true, resources: null } },
    });
    window.dispatchEvent(new Event("rbac-override-change"));
  });
  expect(result.current).toEqual([]);
  await waitFor(() => expect(mocks.fetchAgents).toHaveBeenCalledTimes(2));
  await act(async () => narrowed.resolve(page(false)));
  await waitFor(() =>
    expect(result.current.map((candidate) => candidate.id)).toEqual([
      "agent-identity:agent-example",
    ]),
  );
});
