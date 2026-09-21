import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Gram } from "@gram/client";
import { buildLatestDeploymentQuery } from "@gram/client/react-query/latestDeployment.js";
import { buildListToolsetsQuery } from "@gram/client/react-query/listToolsets.js";

const state = vi.hoisted(() => ({
  projectSlug: "example" as string | undefined,
  isLoading: false,
  grants: [] as Array<{
    scope: string;
    selectors?: Array<Record<string, string>>;
  }>,
  client: {},
  prefetchQuery: vi.fn(
    (_query: { queryKey: readonly unknown[] }) => new Promise<void>(() => {}),
  ),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-a", slug: "example" }),
  useOrganization: () => ({ projects: [{ id: "project-a", slug: "example" }] }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ projectSlug: state.projectSlug }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => state.client,
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({ prefetchQuery: state.prefetchQuery }),
}));
vi.mock("@/hooks/useRBAC", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/hooks/useRBAC")>();
  return {
    ...original,
    useRBAC: () => ({
      grants: state.grants,
      isLoading: state.isLoading,
      hasScope: (
        scope: Parameters<typeof original.hasScopeInGrants>[1],
        id?: string,
      ) => original.hasScopeInGrants(state.grants, scope, id),
    }),
  };
});
import { ProjectResourcePrefetch } from "./ProjectResourcePrefetch";

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.projectSlug = "example";
  state.isLoading = false;
  state.grants = [];
});
const deploymentKey = () =>
  buildLatestDeploymentQuery(state.client as Gram).queryKey;
const toolsetsKey = () =>
  buildListToolsetsQuery(state.client as Gram, { gramProject: "example" })
    .queryKey;
const requestedKeys = () =>
  state.prefetchQuery.mock.calls.map(([query]) => query.queryKey);

describe("ProjectResourcePrefetch", () => {
  it("starts authorized requests in parallel after grants load, using consumer cache keys", () => {
    state.grants = [
      { scope: "project:read", selectors: [{ resourceId: "project-a" }] },
      { scope: "mcp:read" },
    ];
    state.isLoading = true;
    const { rerender } = render(<ProjectResourcePrefetch />);
    expect(state.prefetchQuery).not.toHaveBeenCalled();
    state.isLoading = false;
    rerender(<ProjectResourcePrefetch />);
    // Neither mock promise settles: both requests must have started anyway.
    expect(requestedKeys()).toEqual([deploymentKey(), toolsetsKey()]);
  });

  it("does not treat MCP discovery permission as deployment permission", () => {
    state.grants = [
      {
        scope: "mcp:read",
        selectors: [{ resourceId: "server-a", projectId: "project-a" }],
      },
    ];
    render(<ProjectResourcePrefetch />);
    expect(requestedKeys()).toEqual([toolsetsKey()]);
  });

  it("does not treat project read as MCP discovery permission", () => {
    state.grants = [
      { scope: "project:read", selectors: [{ resourceId: "project-a" }] },
    ];
    render(<ProjectResourcePrefetch />);
    expect(requestedKeys()).toEqual([deploymentKey()]);
  });

  it.each([
    [{ scope: "skill:read" }],
    [
      { scope: "project:read", selectors: [{ resourceId: "project-b" }] },
      { scope: "mcp:read", selectors: [{ projectId: "project-b" }] },
    ],
    [
      { scope: "project:read" },
      { scope: "project:blocked_read" },
      { scope: "mcp:read" },
      { scope: "mcp:blocked_read" },
    ],
  ])("skips unauthorized or excluded resources (%j)", (...grants) => {
    state.grants = grants;
    render(<ProjectResourcePrefetch />);
    expect(state.prefetchQuery).not.toHaveBeenCalled();
  });

  it.each([undefined, "missing-project"])(
    "skips fallback projects for route %s",
    (slug) => {
      state.projectSlug = slug;
      state.grants = [{ scope: "project:read" }, { scope: "mcp:read" }];
      render(<ProjectResourcePrefetch />);
      expect(state.prefetchQuery).not.toHaveBeenCalled();
    },
  );
});
