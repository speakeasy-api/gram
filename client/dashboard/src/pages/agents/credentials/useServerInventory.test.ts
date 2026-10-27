import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useServerInventory } from "./useServerInventory";

const mocks = vi.hoisted(() => ({
  refetch: vi.fn<() => Promise<void>>(),
  activity: [] as {
    targetType: string;
    targetId: string;
    recentToolCalls: number;
  }[],
  servers: [] as {
    id: string;
    visibility: string;
    projectId: string;
    slug: string;
    toolset?: { description: string };
  }[],
}));

vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({}) }));
vi.mock("@tanstack/react-query", () => ({
  useQueries: () => [
    { data: { activity: mocks.activity }, refetch: mocks.refetch },
  ],
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [{ id: "project", name: "Example project", slug: "example" }],
  }),
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

afterEach(cleanup);

it("offers known enabled servers but excludes disabled and unsupported visibility", () => {
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
    "private",
    "public",
  ]);
});

it("removes a server from the inventory when its visibility becomes unsupported", () => {
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

it("orders by calls in the last day, then alphabetically", () => {
  mocks.servers = ["zulu", "alpha", "bravo"].map((slug) => ({
    id: slug,
    slug,
    visibility: "private",
    projectId: "project",
  }));
  mocks.activity = [
    { targetType: "tunneled_mcp_server", targetId: "zulu", recentToolCalls: 4 },
  ];
  const { result } = renderHook(useServerInventory);
  expect(result.current.servers.map((server) => server.id)).toEqual([
    "zulu",
    "alpha",
    "bravo",
  ]);
  mocks.activity = [];
});

it("includes hosted descriptions and retries auxiliary reads", () => {
  mocks.servers = [
    {
      id: "server",
      visibility: "private",
      projectId: "project",
      slug: "example",
      toolset: { description: "Hosted tools" },
    },
  ];
  const { result } = renderHook(useServerInventory);
  expect(result.current.servers[0]?.description).toBe("Hosted tools");
  result.current.refetch();
  expect(mocks.refetch).toHaveBeenCalledTimes(2);
});
