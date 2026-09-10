import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { NetworkAccessSection } from "./NetworkAccessSection";

const testState = vi.hoisted(() => ({
  rolloutStatus: "enabled" as
    | "loading"
    | "enabled"
    | "disabled"
    | "missing"
    | "error",
  entitled: true,
  orgAdmin: true,
  ingressEnabled: true,
  ingressStatus: "online",
  ingressError: false,
  mutate: vi.fn(),
  mutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: () => Promise<void>;
      }
    | undefined,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: testState.rolloutStatus }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => testState.orgAdmin }),
}));

vi.mock("@/hooks/useToolsetUrl", () => ({
  customDomainMcpEndpointUrl: (domain: string, slug: string) =>
    `https://${domain}/mcp/${slug}`,
  useCustomDomains: () => ({
    domains: [{ id: "domain-1", domain: "mcp.example.com" }],
  }),
}));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://platform.example.com",
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { networkIngressEnabled: testState.entitled },
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/networkIngress.js", () => ({
  useNetworkIngress: () => ({
    data: {
      ingress: {
        enabled: testState.ingressEnabled,
        status: testState.ingressStatus,
        dnsName: "private.example.ts.net",
        endpointNamespaceKind: "platform",
      },
    },
    isPending: false,
    isError: testState.ingressError,
  }),
}));

vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  invalidateAllGetMcpServer: vi.fn(),
}));

vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  invalidateAllMcpServers: vi.fn(),
}));

vi.mock("@gram/client/react-query/updateMcpServer.js", () => ({
  useUpdateMcpServerMutation: (options: {
    onError?: (error: Error) => void;
    onSuccess?: () => Promise<void>;
  }) => {
    testState.mutationOptions = options;
    return {
      isPending: false,
      mutate: testState.mutate,
    };
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const baseServer: McpServer = {
  id: "server-1",
  projectId: "project-1",
  name: "Hosted MCP",
  networkAccessMode: "public_only",
  remoteMcpServerId: "remote-1",
  environmentId: "environment-1",
  toolVariationsGroupId: "variation-group-1",
  visibility: "private",
  createdAt: new Date(0),
  updatedAt: new Date(0),
};

const endpoints: McpEndpoint[] = [
  {
    id: "endpoint-platform",
    projectId: "project-1",
    mcpServerId: "server-1",
    slug: "hosted-mcp",
    isDomainRoot: false,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  },
  {
    id: "endpoint-custom",
    projectId: "project-1",
    customDomainId: "domain-1",
    mcpServerId: "server-1",
    slug: "custom-mcp",
    isDomainRoot: true,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  },
];

beforeEach(() => {
  testState.rolloutStatus = "enabled";
  testState.entitled = true;
  testState.orgAdmin = true;
  testState.ingressEnabled = true;
  testState.ingressStatus = "online";
  testState.ingressError = false;
  testState.mutate.mockReset();
  testState.mutationOptions = undefined;
});

afterEach(cleanup);

describe("NetworkAccessSection", () => {
  it.each(["loading", "disabled", "missing", "error"] as const)(
    "renders nothing when the rollout flag is %s",
    (rolloutStatus) => {
      testState.rolloutStatus = rolloutStatus;
      const { container } = render(
        <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
      );

      expect(container.textContent).toBe("");
    },
  );

  it("does not query ingress for a non-admin and reports unavailable state", () => {
    testState.orgAdmin = false;
    render(
      <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
    );

    expect(
      screen.getByText(/Private network availability could not be checked/),
    ).toBeTruthy();
  });

  it("reports unavailable state when the ingress query fails", () => {
    testState.ingressError = true;
    render(
      <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
    );

    expect(
      screen.getByText(/Private network availability could not be checked/),
    ).toBeTruthy();
  });

  it("shows a stored private mode and allows recovery without entitlement", () => {
    testState.entitled = false;
    render(
      <NetworkAccessSection
        mcpServer={{ ...baseServer, networkAccessMode: "private_only" }}
        endpoints={endpoints}
      />,
    );

    expect(
      screen.getByRole("combobox", { name: "Network access mode" }).textContent,
    ).toContain("Private only");
    expect(
      screen.getByText(/You can still switch to public only/),
    ).toBeTruthy();

    fireEvent.click(
      screen.getByRole("combobox", { name: "Network access mode" }),
    );
    fireEvent.click(screen.getByRole("option", { name: /Public only/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        updateMcpServerForm: {
          id: "server-1",
          networkAccessMode: "public_only",
          remoteMcpServerId: "remote-1",
          tunneledMcpServerId: undefined,
          toolsetId: undefined,
          unproxiedMcpServerId: undefined,
          environmentId: "environment-1",
          toolVariationsGroupId: "variation-group-1",
          visibility: "private",
        },
      },
    });
  });

  it("blocks private modes when no endpoint exists in the pinned namespace", () => {
    render(
      <NetworkAccessSection
        mcpServer={baseServer}
        endpoints={endpoints.filter((endpoint) => endpoint.customDomainId)}
      />,
    );

    expect(
      screen.getByText(
        /Add an MCP endpoint in the private ingress's pinned namespace/,
      ),
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole("combobox", { name: "Network access mode" }),
    );
    expect(
      screen
        .getByRole("option", { name: /Private only/ })
        .getAttribute("aria-disabled"),
    ).toBe("true");
  });

  it("requires confirmation for private only and lists affected endpoint URLs", () => {
    render(
      <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
    );

    fireEvent.click(
      screen.getByRole("combobox", { name: "Network access mode" }),
    );
    fireEvent.click(screen.getByRole("option", { name: /Private only/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    const dialogText = screen.getByRole("dialog").textContent;
    expect(dialogText).toContain(
      "Public routes stop serving this MCP server. There is no public fallback.",
    );
    expect(dialogText).toContain(
      "If the tailnet or private ingress loses connectivity",
    );
    expect(dialogText).toContain(
      "Marketplace and device-agent URLs are not rewritten",
    );
    expect(dialogText).toContain("https://platform.example.com/mcp/hosted-mcp");
    expect(dialogText).toContain("https://mcp.example.com/");
    expect(dialogText).toContain("https://mcp.example.com/mcp/custom-mcp");
    expect(dialogText).toContain(
      "https://private.example.ts.net/mcp/hosted-mcp",
    );
    expect(dialogText).not.toContain(
      "https://private.example.ts.net/mcp/custom-mcp",
    );
    expect(testState.mutate).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Make private only" }));
    expect(testState.mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        request: expect.objectContaining({
          updateMcpServerForm: expect.objectContaining({
            networkAccessMode: "private_only",
          }),
        }),
      }),
    );
  });
});
