import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

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
const domainsLoading = vi.hoisted(() => ({ current: false }));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useCustomDomains: () => ({
    isLoading: domainsLoading.current,
    domains: domainsLoading.current
      ? []
      : [
          {
            id: "domain-1",
            domain: "mcp.example.com",
            verified: true,
            activated: true,
          },
        ],
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
vi.mock("../../../useFollowRenamedServer", () => ({
  useFollowRenamedServer: () => vi.fn(),
}));
vi.mock("@/hooks/useNetworkIngressRollout", () => ({
  useNetworkIngressRollout: () => ({ canManageIngress: true }),
}));
const networkIngress = vi.hoisted(() => ({
  current: undefined as
    | { endpointNamespaceKind: string; customDomainId?: string }
    | undefined,
}));
vi.mock("@gram/client/react-query/networkIngress.js", () => ({
  useNetworkIngress: () => ({ data: { ingress: networkIngress.current } }),
}));
vi.mock("../../../useMcpEndpointSlugValidation", () => ({
  useMcpEndpointSlugValidation: () => null,
}));
vi.mock("@gram/client/react-query/deleteMcpEndpoint.js", () => ({
  useDeleteMcpEndpointMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));
const updateEndpoint = vi.hoisted(() => vi.fn());
vi.mock("@gram/client/react-query/updateMcpEndpoint.js", () => ({
  useUpdateMcpEndpointMutation: () => ({
    isPending: false,
    isError: false,
    mutate: updateEndpoint,
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

afterEach(() => {
  cleanup();
  updateEndpoint.mockReset();
  domainsLoading.current = false;
  networkIngress.current = undefined;
});

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

  it("offers to add an address to a canonical server that has none", () => {
    renderSection(server({ id: "toolset-1", toolsetId: "toolset-1" }), []);

    expect(screen.getByText("No hosted address")).toBeTruthy();
    expect(
      screen.getAllByRole("button", { name: "Add" }).length,
    ).toBeGreaterThan(0);
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

  it("moves a canonical server's platform address onto the verified domain in place", () => {
    renderSection(server({ id: "toolset-1", toolsetId: "toolset-1" }), [
      platformEndpoint,
    ]);

    fireEvent.click(
      screen.getByRole("button", { name: "Move to mcp.example.com" }),
    );
    expect(updateEndpoint).toHaveBeenCalledWith({
      request: {
        updateMcpEndpointForm: {
          id: "endpoint-1",
          mcpServerId: "toolset-1",
          slug: "acme-example",
          customDomainId: "domain-1",
        },
      },
    });
  });

  it("moves a canonical server back to the platform only once its domain cannot serve", () => {
    renderSection(server({ id: "toolset-1", toolsetId: "toolset-1" }), [
      customEndpoint,
    ]);
    expect(
      screen.queryByRole("button", { name: "Move to Speakeasy domain" }),
    ).toBeNull();
    cleanup();

    renderSection(server({ id: "toolset-1", toolsetId: "toolset-1" }), [
      { ...customEndpoint, customDomainId: "deleted-domain" },
    ]);
    fireEvent.click(
      screen.getByRole("button", { name: "Move to Speakeasy domain" }),
    );
    expect(updateEndpoint).toHaveBeenCalledWith({
      request: {
        updateMcpEndpointForm: {
          id: "endpoint-2",
          mcpServerId: "toolset-1",
          slug: "acme-example",
          customDomainId: undefined,
        },
      },
    });
  });

  it("does not treat a still-loading domain list as a deleted domain", () => {
    domainsLoading.current = true;
    renderSection(server({ id: "toolset-1", toolsetId: "toolset-1" }), [
      customEndpoint,
    ]);

    expect(
      screen.queryByRole("button", { name: "Move to Speakeasy domain" }),
    ).toBeNull();
  });

  it("only offers a private server moves within its ingress namespace", () => {
    networkIngress.current = { endpointNamespaceKind: "platform" };
    renderSection(
      server({
        id: "toolset-1",
        toolsetId: "toolset-1",
        networkAccessMode: "private_only",
      }),
      [platformEndpoint],
    );
    expect(screen.queryByRole("button", { name: /^Move to/ })).toBeNull();
    cleanup();

    networkIngress.current = {
      endpointNamespaceKind: "custom_domain",
      customDomainId: "domain-1",
    };
    renderSection(
      server({
        id: "toolset-1",
        toolsetId: "toolset-1",
        networkAccessMode: "private_only",
      }),
      [platformEndpoint],
    );
    expect(
      screen.getByRole("button", { name: "Move to mcp.example.com" }),
    ).toBeTruthy();
  });

  it("does not offer in-place moves to other servers", () => {
    renderSection(server({ toolsetId: "toolset-1" }), [platformEndpoint]);

    expect(screen.queryByRole("button", { name: /^Move to/ })).toBeNull();
  });
});
