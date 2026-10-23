import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";

import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { ServerUrlSection } from "./ServerUrlSection";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useCustomDomains: () => ({
    domains: [{ id: "domain-1", domain: "mcp.example.com" }],
  }),
}));
vi.mock("@/hooks/usePrivateMcpServerUrls", () => ({
  usePrivateMcpServerUrls: () => ({
    privateMcpUrls: [],
    privateInstallPageUrls: [],
    isLoading: false,
    isError: false,
  }),
}));
vi.mock("@/hooks/useRootMcpEndpoint", () => ({
  invalidateRootMcpEndpointQueries: vi.fn(),
  patchMcpEndpointInCache: vi.fn(),
  useRootMcpEndpointMutation: () => ({
    isPending: false,
    setRootMcpEndpoint: vi.fn(),
  }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ domains: { goTo: vi.fn() } }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({}),
  useSlugs: () => ({ orgSlug: "acme" }),
}));
vi.mock("../../../useMcpEndpointSlugValidation", () => ({
  useMcpEndpointSlugValidation: () => null,
}));
vi.mock("@gram/client/react-query/deleteMcpEndpoint.js", () => ({
  useDeleteMcpEndpointMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock("@gram/client/react-query/updateMcpEndpoint.js", () => ({
  useUpdateMcpEndpointMutation: () => ({
    isPending: false,
    isError: false,
    mutate: vi.fn(),
  }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

function server(overrides: Partial<McpServer>): McpServer {
  return {
    id: "mcp-server-1",
    projectId: "project-1",
    name: "Example server",
    networkAccessMode: "public_only",
    visibility: "private",
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  } as McpServer;
}

const platformEndpoint = {
  id: "endpoint-1",
  slug: "acme-example",
} as McpEndpoint;

const customEndpoint = {
  id: "endpoint-2",
  slug: "example",
  customDomainId: "domain-1",
} as McpEndpoint;

function renderSection(mcpServer: McpServer, endpoints: McpEndpoint[]) {
  render(
    <ServerUrlSection
      backend={{ mcpServerId: mcpServer.id }}
      endpoints={endpoints}
      isLoadingEndpoints={false}
      mcpServer={mcpServer}
    />,
  );
}

afterEach(cleanup);

describe("ServerUrlSection", () => {
  it("lets a canonical hosted server edit its one address but not add or remove", () => {
    renderSection(server({ id: "toolset-1", toolsetId: "toolset-1" }), [
      platformEndpoint,
    ]);

    expect(screen.getByLabelText("Save address")).toBeTruthy();
    expect(screen.getByDisplayValue("example")).toBeTruthy();
    expect(screen.queryByLabelText("Remove address")).toBeNull();
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
    expect(screen.queryByText("Custom Address")).toBeNull();
  });

  it("hides add and remove for a canonical server on a custom domain", () => {
    renderSection(server({ id: "toolset-1", toolsetId: "toolset-1" }), [
      customEndpoint,
    ]);

    expect(screen.getByLabelText("Save address")).toBeTruthy();
    expect(screen.queryByLabelText("Remove address")).toBeNull();
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
    expect(screen.queryByText("Hosted Address")).toBeNull();
  });

  it("keeps add and remove for other servers", () => {
    renderSection(server({ toolsetId: "toolset-1" }), [
      platformEndpoint,
      customEndpoint,
    ]);

    expect(screen.getAllByLabelText("Save address")).toHaveLength(2);
    expect(screen.getAllByLabelText("Remove address")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Add" })).toBeTruthy();
  });
});
