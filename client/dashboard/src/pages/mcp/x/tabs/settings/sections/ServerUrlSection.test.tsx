import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { ServerUrlSection } from "./ServerUrlSection";
import { TooltipProvider } from "@/components/ui/Tooltip";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => true }),
}));
const domainsOverride = vi.hoisted(() => ({
  current: undefined as
    | Array<{
        id: string;
        domain: string;
        verified: boolean;
        activated: boolean;
      }>
    | undefined,
}));
vi.mock("@/hooks/useToolsetUrl", () => ({
  useCustomDomains: () => ({
    domains: domainsOverride.current ?? [
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
const slugValidation = vi.hoisted(() => vi.fn((..._args: unknown[]) => null));
vi.mock("../../../useMcpEndpointSlugValidation", () => ({
  useMcpEndpointSlugValidation: slugValidation,
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
    <TooltipProvider>
      <ServerUrlSection
        backend={{ mcpServerId: mcpServer.id }}
        endpoints={endpoints}
        isLoadingEndpoints={false}
        mcpServer={mcpServer}
      />
    </TooltipProvider>,
  );
}

afterEach(() => {
  cleanup();
  deleteEndpoint.mockReset();
  networkIngress.current = undefined;
  domainsOverride.current = undefined;
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
    renderSection(
      server({
        id: "toolset-1",
        toolsetId: "toolset-1",
        visibility: "disabled",
      }),
      [platformEndpoint, dead],
    );

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

  it("shows the legacy Speakeasy address a custom-domain alias still serves", () => {
    renderSection(
      server({
        id: "toolset-1",
        toolsetId: "toolset-1",
        legacyPlatformSlug: "example",
      }),
      [customEndpoint],
    );

    expect(screen.queryByText("No hosted address")).toBeNull();
    expect(screen.getByText(/\/mcp\/example$/)).toBeTruthy();
    expect(screen.getByLabelText("Add hosted address")).toBeTruthy();
    cleanup();

    renderSection(
      server({
        id: "server-2",
        toolsetId: "toolset-1",
        legacyPlatformSlug: "example",
      }),
      [customEndpoint],
    );
    expect(
      screen.queryByText(/\/mcp\/example$/),
      "only the canonical server shows the legacy address",
    ).toBeNull();
  });

  it("lets a hosted server add another platform address", () => {
    renderSection(hosted(), [platformEndpoint]);

    fireEvent.click(screen.getByLabelText("Add hosted address"));
    expect(screen.getByLabelText("Add address")).toBeTruthy();
  });

  it("lets a private hosted server on a platform ingress add a platform address", () => {
    networkIngress.current = { endpointNamespaceKind: "platform" };
    renderSection(
      server({
        id: "toolset-1",
        toolsetId: "toolset-1",
        networkAccessMode: "dual",
      }),
      [platformEndpoint],
    );

    expect(screen.getByLabelText("Add hosted address")).toBeTruthy();
  });

  it("asks to disable an enabled hosted server before removing its last address", () => {
    renderSection(hosted(), [platformEndpoint]);

    fireEvent.click(screen.getByLabelText("Remove address"));
    expect(screen.getByText("Disable this server first")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Remove address" })).toBeNull();
    fireEvent.click(screen.getAllByRole("button", { name: "Close" }).at(-1)!);
    expect(deleteEndpoint).not.toHaveBeenCalled();
  });

  it("removes the last address of a disabled hosted server after confirming", () => {
    renderSection(
      server({
        id: "toolset-1",
        toolsetId: "toolset-1",
        visibility: "disabled",
      }),
      [platformEndpoint],
    );

    fireEvent.click(screen.getByLabelText("Remove address"));
    fireEvent.click(screen.getByRole("button", { name: "Remove address" }));
    expect(deleteEndpoint).toHaveBeenCalledWith({
      request: { id: "endpoint-1" },
    });
  });

  it("only offers verified, activated domains for a new address", () => {
    domainsOverride.current = [
      {
        id: "domain-2",
        domain: "pending.example.com",
        verified: false,
        activated: false,
      },
      {
        id: "domain-1",
        domain: "mcp.example.com",
        verified: true,
        activated: true,
      },
    ];
    renderSection(server({ toolsetId: "toolset-1" }), [platformEndpoint]);

    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(screen.queryByText(/pending\.example\.com/)).toBeNull();
  });

  it("says no live domain is available to a public hosted server with only pending domains", () => {
    domainsOverride.current = [
      {
        id: "domain-2",
        domain: "pending.example.com",
        verified: false,
        activated: false,
      },
    ];
    renderSection(hosted(), [platformEndpoint]);

    expect(
      screen.getByText("No verified, activated custom domain is available."),
    ).toBeTruthy();
  });

  it("passes the hosted flag to slug validation for a hosted server's custom addresses", () => {
    renderSection(hosted(), [platformEndpoint, customEndpoint]);

    expect(slugValidation).toHaveBeenCalledWith(
      "example",
      "domain-1",
      "example",
      true,
      "toolset-1",
    );
  });

  it("checks a new hosted address against the server so its own legacy alias reads as available", () => {
    renderSection(hosted(), [platformEndpoint]);
    slugValidation.mockClear();

    fireEvent.click(screen.getByLabelText("Add hosted address"));
    expect(slugValidation).toHaveBeenCalledWith(
      expect.any(String),
      null,
      undefined,
      false,
      "toolset-1",
    );
  });
});
