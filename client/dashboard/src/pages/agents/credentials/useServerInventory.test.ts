import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useServerInventory } from "./useServerInventory";

const mocks = vi.hoisted(() => ({
  servers: [] as {
    id: string;
    visibility: string;
    projectId: string;
    slug: string;
    toolsetId?: string;
  }[],
  grants: [] as {
    scope: string;
    selectors?: {
      resourceKind: string;
      resourceId: string;
      projectId?: string;
    }[];
  }[],
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "project", name: "Example project", slug: "example" }],
  }),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    grants: mocks.grants,
    isLoading: false,
  }),
  hasScopeInGrants: (
    grants: typeof mocks.grants,
    scope: string,
    resourceId: string,
    projectId?: string,
  ) => {
    if (scope !== "mcp:connect") return false;
    return grants.some((grant) => {
      if (grant.scope !== "mcp:connect") return false;
      if (!grant.selectors) return true;
      return grant.selectors.some((selector) => {
        if (selector.resourceKind !== "mcp") return false;
        if (selector.resourceId !== "*" && selector.resourceId !== resourceId)
          return false;
        if (selector.projectId && selector.projectId !== projectId)
          return false;
        return true;
      });
    });
  },
}));
vi.mock("@gram/client/react-query/listMcpServersForOrg.js", () => ({
  useListMcpServersForOrg: () => ({
    data: { mcpServers: mocks.servers },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/listToolsetsForOrg.js", () => ({
  useListToolsetsForOrg: () => ({
    data: { toolsets: [] },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

afterEach(() => {
  cleanup();
  mocks.grants = [];
  mocks.servers = [];
});

it("offers known enabled servers but excludes disabled and unsupported visibility", () => {
  mocks.grants = [{ scope: "mcp:connect" }];
  mocks.servers = ["public", "private", "disabled", "future_visibility"].map(
    (visibility) => ({
      id: visibility,
      visibility,
      slug: visibility,
      projectId: "project",
    }),
  );
  const { result } = renderHook(useServerInventory);
  expect(result.current.servers.map((server) => server.id)).toEqual([
    "public",
    "private",
  ]);
});

it("removes a server from the inventory when its visibility becomes unsupported", () => {
  mocks.grants = [{ scope: "mcp:connect" }];
  mocks.servers = [
    {
      id: "server",
      visibility: "private",
      slug: "example",
      projectId: "project",
    },
  ];
  const { result, rerender } = renderHook(useServerInventory);
  expect(result.current.servers).toHaveLength(1);
  mocks.servers = [{ ...mocks.servers[0]!, visibility: "future_visibility" }];
  rerender();
  expect(result.current.servers).toEqual([]);
});

it("excludes servers the user does not have mcp:connect permission on", () => {
  mocks.servers = [
    {
      id: "accessible",
      visibility: "public",
      slug: "accessible",
      projectId: "project",
    },
    {
      id: "inaccessible",
      visibility: "public",
      slug: "inaccessible",
      projectId: "project",
    },
  ];
  mocks.grants = [
    {
      scope: "mcp:connect",
      selectors: [{ resourceKind: "mcp", resourceId: "accessible" }],
    },
  ];
  const { result } = renderHook(useServerInventory);
  expect(result.current.servers.map((server) => server.id)).toEqual([
    "accessible",
  ]);
});

it("includes all servers when user has wildcard mcp:connect grant", () => {
  mocks.servers = [
    {
      id: "server_a",
      visibility: "public",
      slug: "server-a",
      projectId: "project",
    },
    {
      id: "server_b",
      visibility: "public",
      slug: "server-b",
      projectId: "project",
    },
  ];
  mocks.grants = [
    {
      scope: "mcp:connect",
      selectors: [{ resourceKind: "mcp", resourceId: "*" }],
    },
  ];
  const { result } = renderHook(useServerInventory);
  expect(result.current.servers.map((server) => server.id)).toEqual([
    "server_a",
    "server_b",
  ]);
});

it("excludes all servers when user has no mcp:connect grants", () => {
  mocks.servers = [
    {
      id: "server_a",
      visibility: "public",
      slug: "server-a",
      projectId: "project",
    },
    {
      id: "server_b",
      visibility: "public",
      slug: "server-b",
      projectId: "project",
    },
  ];
  mocks.grants = [];
  const { result } = renderHook(useServerInventory);
  expect(result.current.servers).toEqual([]);
});

it("filters by project when grant is project-scoped", () => {
  mocks.servers = [
    {
      id: "server_a",
      visibility: "public",
      slug: "server-a",
      projectId: "project",
    },
    {
      id: "server_b",
      visibility: "public",
      slug: "server-b",
      projectId: "other_project",
    },
  ];
  mocks.grants = [
    {
      scope: "mcp:connect",
      selectors: [
        { resourceKind: "mcp", resourceId: "*", projectId: "project" },
      ],
    },
  ];
  const { result } = renderHook(useServerInventory);
  expect(result.current.servers.map((server) => server.id)).toEqual([
    "server_a",
  ]);
});
