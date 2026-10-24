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
vi.mock("@/hooks/useToolsetUrl", () => ({
  useCustomDomains: () => ({
    domains: [
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
const deleteEndpoint = vi.hoisted(() => vi.fn());
vi.mock("@gram/client/react-query/deleteMcpEndpoint.js", () => ({
  useDeleteMcpEndpointMutation: () => ({
    isPending: false,
    mutate: deleteEndpoint,
  }),
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

afterEach(() => {
  cleanup();
  deleteEndpoint.mockReset();
  networkIngress.current = undefined;
});

describe("ServerUrlSection", () => {
  const hosted = () => server({ id: "toolset-1", toolsetId: "toolset-1" });

  it("lets a hosted server add and remove addresses", () => {
    renderSection(hosted(), [platformEndpoint, customEndpoint]);

    expect(screen.getAllByLabelText("Save address")).toHaveLength(2);
    expect(screen.getAllByLabelText("Remove address")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Add" })).toBeTruthy();
  });

  it("lists every platform address of a hosted server so each can be removed", () => {
    const second = { id: "endpoint-3", slug: "acme-second" } as McpEndpoint;
    renderSection(hosted(), [platformEndpoint, second]);

    expect(screen.getAllByLabelText("Remove address")).toHaveLength(2);
    fireEvent.click(screen.getAllByLabelText("Remove address")[1]!);
    expect(deleteEndpoint).toHaveBeenCalledWith({
      request: { id: "endpoint-3" },
    });
  });

  it("confirms before removing the last address that can serve", () => {
    const dead = {
      id: "endpoint-4",
      slug: "stale",
      customDomainId: "deleted-domain",
    } as McpEndpoint;
    renderSection(hosted(), [platformEndpoint, dead]);

    const [platformRemove, deadRemove] =
      screen.getAllByLabelText("Remove address");
    fireEvent.click(deadRemove!);
    expect(deleteEndpoint).toHaveBeenCalledWith({
      request: { id: "endpoint-4" },
    });
    deleteEndpoint.mockReset();
    fireEvent.click(platformRemove!);
    expect(deleteEndpoint).not.toHaveBeenCalled();
    expect(screen.getByText("Remove this server's only address?")).toBeTruthy();
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

  it("only offers a private hosted server addresses in its ingress namespace", () => {
    const privateHosted = server({
      id: "toolset-1",
      toolsetId: "toolset-1",
      networkAccessMode: "private_only",
    });
    networkIngress.current = { endpointNamespaceKind: "platform" };
    renderSection(privateHosted, []);
    expect(
      screen.getByRole("button", { name: "Add" }),
      "only the hosted address can be added",
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Private network access keeps this server's addresses on its ingress namespace.",
      ),
    ).toBeTruthy();
    cleanup();

    networkIngress.current = {
      endpointNamespaceKind: "custom_domain",
      customDomainId: "domain-1",
    };
    renderSection(privateHosted, []);
    expect(screen.getAllByRole("button", { name: "Add" })).toHaveLength(1);
    expect(screen.getByText("No hosted address")).toBeTruthy();
    expect(screen.getByText("No custom address")).toBeTruthy();
  });
});
