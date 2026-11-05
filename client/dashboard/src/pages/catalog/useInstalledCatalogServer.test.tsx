import { renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useInstalledCatalogServer } from "./useInstalledCatalogServer";
import { queryKeyListMCPCatalog } from "@gram/client/react-query/listMCPCatalog.js";
const mocks = vi.hoisted(() => ({
  servers: vi.fn(),
  remotes: vi.fn(),
  unproxied: vi.fn(),
  deployment: vi.fn(),
  query: vi.fn(),
  listCatalog: vi.fn(),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: (...args: unknown[]) => {
    mocks.servers(...args);
    return {
      data: { mcpServers: [{ id: "server", remoteMcpServerId: "remote" }] },
    };
  },
}));
vi.mock("@gram/client/react-query/remoteMcpServers.js", () => ({
  useRemoteMcpServers: (...args: unknown[]) => {
    mocks.remotes(...args);
    return {
      data: {
        remoteMcpServers: [{ id: "remote", url: "https://example.com/mcp" }],
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
vi.mock("@gram/client/react-query/latestDeployment.js", () => ({
  useLatestDeployment: (...args: unknown[]) => {
    mocks.deployment(...args);
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
    return {};
  },
}));
it("scopes every lookup and the catalog cache/request to the server project", async () => {
  renderHook(() => useInstalledCatalogServer("server", true, "server-project"));
  for (const query of [
    mocks.servers,
    mocks.remotes,
    mocks.unproxied,
    mocks.deployment,
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
