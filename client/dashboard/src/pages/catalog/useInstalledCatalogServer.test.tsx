import { renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { useInstalledCatalogServer } from "./useInstalledCatalogServer";
import { queryKeyListMCPCatalog } from "@gram/client/react-query/listMCPCatalog.js";
const mocks = vi.hoisted(() => ({
  server: { id: "server", remoteMcpServerId: "remote" } as Record<
    string,
    unknown
  >,
  catalog: [] as unknown[],
  servers: vi.fn(),
  remotes: vi.fn(),
  unproxied: vi.fn(),
  query: vi.fn(),
  listCatalog: vi.fn(),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: (...args: unknown[]) => {
    mocks.servers(...args);
    return {
      data: { mcpServers: [mocks.server] },
    };
  },
}));
vi.mock("@gram/client/react-query/remoteMcpServers.js", () => ({
  useRemoteMcpServers: (...args: unknown[]) => {
    mocks.remotes(...args);
    return {
      data: {
        remoteMcpServers: [{ id: "remote", url: "https://example.com/mcp///" }],
      },
    };
  },
}));
vi.mock("@gram/client/react-query/unproxiedMcpServers.js", () => ({
  useUnproxiedMcpServers: (...args: unknown[]) => {
    mocks.unproxied(...args);
    return {};
  },
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ mcpRegistries: { listCatalog: mocks.listCatalog } }),
  useProjectSlugForRequests: () => "selected-project",
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: (options: unknown) => {
    mocks.query(options);
    return { data: { servers: mocks.catalog } };
  },
}));
beforeEach(() => {
  vi.clearAllMocks();
  mocks.server = { id: "server", remoteMcpServerId: "remote" };
  mocks.catalog = [];
});
it("scopes every lookup and the catalog cache/request to the server project", async () => {
  renderHook(() => useInstalledCatalogServer("server", true, "server-project"));
  for (const query of [
    mocks.servers,
    mocks.remotes,
    mocks.unproxied,
  ]) {
    expect(query).toHaveBeenCalledWith(
      { gramProject: "server-project" },
      undefined,
      expect.anything(),
    );
  }
  const options = mocks.query.mock.lastCall![0];
  expect(options.queryKey).toEqual(
    queryKeyListMCPCatalog({
      gramProject: "server-project",
      search: undefined,
      registryId: undefined,
    }),
  );
  await options.queryFn();
  expect(mocks.listCatalog).toHaveBeenCalledWith({
    gramProject: "server-project",
    search: undefined,
    registryId: undefined,
  });
});

it("normalizes all trailing slashes for installed remotes", () => {
  const entry = {
    registrySpecifier: "remote",
    remotes: [{ url: "https://example.com/mcp" }],
  };
  mocks.catalog = [entry];
  expect(
    renderHook(() => useInstalledCatalogServer("server")).result.current,
  ).toEqual(entry);
});
