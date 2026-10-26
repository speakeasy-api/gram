import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useServerInventory } from "./useServerInventory";

const mocks = vi.hoisted(() => ({
  servers: [] as {
    id: string;
    visibility: string;
    projectId: string;
    slug: string;
  }[],
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
    "public",
    "private",
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
