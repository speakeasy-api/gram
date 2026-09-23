import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { NetworkAccessSection } from "./NetworkAccessSection";

const testState = vi.hoisted(() => ({
  entitled: true,
  productTier: "enterprise" as "enterprise" | "payg" | "base",
  featureStatus: "success" as "pending" | "success" | "error",
  rolloutStatus: undefined as
    | "loading"
    | "enabled"
    | "disabled"
    | "error"
    | undefined,
  featureFetching: false,
  orgAdmin: true,
  ingressEnabled: true,
  ingressStatus: "online",
  ingressQueryStatus: "success" as "pending" | "success" | "error",
  ingressFetching: false,
  domainsStatus: "success" as "pending" | "success" | "error",
  domainsFetching: false,
  domains: [{ id: "domain-1", domain: "mcp.example.com" }],
  ingressQuery: vi.fn(),
  requireScopeProps: undefined as
    | { scope: string; resourceId?: string; level: string }
    | undefined,
  mutate: vi.fn(),
  mutateGateway: vi.fn(),
  invalidateGetGateway: vi.fn().mockResolvedValue(undefined),
  invalidateListGateway: vi.fn().mockResolvedValue(undefined),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  mutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: () => Promise<void>;
      }
    | undefined,
  gatewayMutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: () => Promise<void>;
      }
    | undefined,
}));

vi.mock("@/components/ui/CopyButton", () => ({
  CopyButton: ({ text }: { text: string }) => (
    <button type="button" aria-label={`Copy ${text}`} />
  ),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    ...props
  }: {
    children: React.ReactNode;
    scope: string;
    resourceId?: string;
    level: string;
  }) => {
    testState.requireScopeProps = props;
    return <>{children}</>;
  },
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => testState.productTier,
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1" }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => testState.orgAdmin }),
}));

vi.mock("@/hooks/useNetworkIngressRollout", () => ({
  useNetworkIngressRollout: () => {
    const status =
      testState.rolloutStatus ??
      (testState.featureStatus === "pending"
        ? "loading"
        : testState.featureStatus === "error"
          ? "error"
          : testState.entitled
            ? "enabled"
            : "disabled");

    return {
      status,
      rolloutEnabled: status === "enabled",
      canManageIngress: testState.orgAdmin,
    };
  },
}));

vi.mock("@/hooks/useToolsetUrl", () => ({
  customDomainMcpEndpointUrl: (domain: string, slug: string) =>
    `https://${domain}/mcp/${slug}`,
}));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => "https://platform.example.com",
}));

vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: () => ({
    data: { networkIngressEnabled: testState.entitled },
    isPending: testState.featureStatus === "pending",
    isError: testState.featureStatus === "error",
    isSuccess: testState.featureStatus === "success",
    isFetching: testState.featureFetching,
  }),
}));

vi.mock("@gram/client/react-query/networkIngress.js", () => ({
  useNetworkIngress: (...args: unknown[]) => {
    testState.ingressQuery(...args);
    const options = args[2] as { enabled?: boolean } | undefined;
    const enabled = options?.enabled !== false;
    return {
      data:
        enabled && testState.ingressQueryStatus !== "pending"
          ? {
              ingress: {
                enabled: testState.ingressEnabled,
                status: testState.ingressStatus,
                dnsName: "private.example.ts.net",
                endpointNamespaceKind: "platform",
              },
            }
          : undefined,
      isPending: enabled && testState.ingressQueryStatus === "pending",
      isError: enabled && testState.ingressQueryStatus === "error",
      isSuccess: enabled && testState.ingressQueryStatus === "success",
      isFetching: enabled && testState.ingressFetching,
    };
  },
}));

vi.mock("@gram/client/react-query/listDomains.js", () => ({
  useListDomains: () => ({
    data:
      testState.domainsStatus === "pending"
        ? undefined
        : { domains: testState.domains },
    isPending: testState.domainsStatus === "pending",
    isLoading: testState.domainsStatus === "pending",
    isFetching: testState.domainsFetching,
    isError: testState.domainsStatus === "error",
    isSuccess: testState.domainsStatus === "success",
  }),
}));

vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  invalidateAllGetMcpServer: vi.fn(),
}));
vi.mock("@gram/client/react-query/getMetaMcpServer.js", () => ({
  invalidateAllGetMetaMcpServer: testState.invalidateGetGateway,
}));
vi.mock("@gram/client/react-query/metaMcpServers.js", () => ({
  invalidateAllMetaMcpServers: testState.invalidateListGateway,
}));
vi.mock("@gram/client/react-query/updateMetaMcpServer.js", () => ({
  useUpdateMetaMcpServerMutation: (options: {
    onError?: (error: Error) => void;
    onSuccess?: () => Promise<void>;
  }) => {
    testState.gatewayMutationOptions = options;
    return {
      isPending: false,
      mutate: testState.mutateGateway,
    };
  },
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
  toast: { error: testState.toastError, success: testState.toastSuccess },
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

const gateway: MetaMcpServer = {
  id: "gateway-1",
  name: "My Gateway",
  organizationId: "org-1",
  projectId: "project-1",
  networkAccessMode: "public_only",
  visibility: "private",
  createdAt: new Date(0),
  updatedAt: new Date(0),
};

const endpoints: [McpEndpoint, McpEndpoint] = [
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
  testState.entitled = true;
  testState.productTier = "enterprise";
  testState.featureStatus = "success";
  testState.rolloutStatus = undefined;
  testState.featureFetching = false;
  testState.orgAdmin = true;
  testState.ingressEnabled = true;
  testState.ingressStatus = "online";
  testState.ingressQueryStatus = "success";
  testState.ingressFetching = false;
  testState.domainsStatus = "success";
  testState.domainsFetching = false;
  testState.domains = [{ id: "domain-1", domain: "mcp.example.com" }];
  testState.ingressQuery.mockReset();
  testState.requireScopeProps = undefined;
  testState.mutate.mockReset();
  testState.mutateGateway.mockReset();
  testState.invalidateGetGateway.mockClear();
  testState.invalidateListGateway.mockClear();
  testState.toastSuccess.mockClear();
  testState.toastError.mockClear();
  testState.mutationOptions = undefined;
  testState.gatewayMutationOptions = undefined;
});

afterEach(cleanup);

describe("NetworkAccessSection", () => {
  it.each(["base", "payg"] as const)(
    "blocks private choices for %s even with staff entitlement",
    (tier) => {
      testState.productTier = tier;
      render(
        <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
      );
      expect(screen.getByText(/available on the Enterprise plan/)).toBeTruthy();
      expect(
        screen
          .getByRole("link", { name: "Talk to our team about upgrading" })
          .getAttribute("href"),
      ).toBe("https://www.speakeasy.com/book-demo");
      fireEvent.click(
        screen.getByRole("combobox", { name: "Network access mode" }),
      );
      expect(
        screen
          .getByRole("option", { name: /Public and private/ })
          .getAttribute("data-disabled"),
      ).toBe("");
      expect(
        screen
          .getByRole("option", { name: /Private only/ })
          .getAttribute("data-disabled"),
      ).toBe("");
    },
  );

  it("hides public-only network access for a non-admin without rollout", () => {
    testState.orgAdmin = false;
    testState.entitled = false;
    const { container } = render(
      <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
    );
    expect(container.innerHTML).toBe("");
  });

  it("shows network access before staff enables Tailscale", () => {
    testState.entitled = false;
    render(
      <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
    );
    expect(
      screen.getByRole("combobox", { name: "Network access mode" }),
    ).toBeTruthy();
  });

  it.each(["loading", "error"] as const)(
    "keeps the section visible while entitlement lookup is %s",
    (status) => {
      testState.rolloutStatus = status;
      testState.featureStatus = status === "loading" ? "pending" : "error";

      render(
        <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
      );

      expect(
        screen.getByRole("combobox", { name: "Network access mode" }),
      ).toBeTruthy();
      expect(
        screen.getByText(
          status === "loading"
            ? /Checking private network availability/
            : /Private network availability could not be checked/,
        ),
      ).toBeTruthy();
    },
  );

  it("keeps a stored private mode visible when the staff entitlement is disabled", () => {
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
      screen.getByText("https://private.example.ts.net/mcp/hosted-mcp"),
    ).toBeTruthy();
  });

  it("does not query ingress for a non-admin and reports unavailable state", () => {
    testState.orgAdmin = false;
    render(
      <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
    );

    expect(testState.ingressQuery).toHaveBeenCalledWith(
      undefined,
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(
      screen.getByText(/Private network availability could not be checked/),
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

  it("hides cached private URLs after admin access is revoked", () => {
    testState.orgAdmin = false;
    render(
      <NetworkAccessSection
        mcpServer={{ ...baseServer, networkAccessMode: "private_only" }}
        endpoints={endpoints}
      />,
    );

    expect(
      screen.queryByText("https://private.example.ts.net/mcp/hosted-mcp"),
    ).toBeNull();
  });

  it("reports unavailable state when the ingress query fails", () => {
    testState.ingressQueryStatus = "error";
    render(
      <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
    );

    expect(
      screen.getByText(/Private network availability could not be checked/),
    ).toBeTruthy();
  });

  it.each(["pending", "error"] as const)(
    "blocks private choices when the product-feature query is %s, even with cached entitlement data",
    (status) => {
      testState.featureStatus = status;
      render(
        <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
      );

      fireEvent.click(
        screen.getByRole("combobox", { name: "Network access mode" }),
      );
      expect(
        screen
          .getByRole("option", { name: /Private only/ })
          .getAttribute("aria-disabled"),
      ).toBe("true");
    },
  );

  it.each(["pending", "error"] as const)(
    "blocks private choices when the ingress query is %s, even with cached ingress data",
    (status) => {
      testState.ingressQueryStatus = status;
      render(
        <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
      );

      fireEvent.click(
        screen.getByRole("combobox", { name: "Network access mode" }),
      );
      expect(
        screen
          .getByRole("option", { name: /Private only/ })
          .getAttribute("aria-disabled"),
      ).toBe("true");
    },
  );

  it("hides cached private URLs while ingress data refetches", () => {
    testState.ingressFetching = true;
    render(
      <NetworkAccessSection
        mcpServer={{ ...baseServer, networkAccessMode: "private_only" }}
        endpoints={endpoints}
      />,
    );

    expect(
      screen.queryByText("https://private.example.ts.net/mcp/hosted-mcp"),
    ).toBeNull();
  });

  it.each(["features", "ingress"] as const)(
    "keeps private choices available while successful %s data refetches",
    (query) => {
      if (query === "features") {
        testState.featureFetching = true;
      } else {
        testState.ingressFetching = true;
      }
      render(
        <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
      );

      fireEvent.click(
        screen.getByRole("combobox", { name: "Network access mode" }),
      );
      expect(
        screen
          .getByRole("option", { name: /Private only/ })
          .getAttribute("aria-disabled"),
      ).not.toBe("true");
      expect(
        screen.queryByText(/Private network availability could not be checked/),
      ).toBeNull();
    },
  );

  it("checks mcp:write against the current project", () => {
    render(
      <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
    );

    expect(testState.requireScopeProps).toEqual({
      scope: "mcp:write",
      resourceId: "project-1",
      level: "component",
    });
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

  it.each(["pending", "refetching", "error", "missing"] as const)(
    "fails closed when custom domains become %s before private-only confirmation",
    (status) => {
      const { rerender } = render(
        <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
      );

      fireEvent.click(
        screen.getByRole("combobox", { name: "Network access mode" }),
      );
      fireEvent.click(screen.getByRole("option", { name: /Private only/ }));
      fireEvent.click(screen.getByRole("button", { name: "Save" }));
      expect(screen.getByRole("dialog")).toBeTruthy();

      if (status === "missing") {
        testState.domains = [];
      } else if (status === "refetching") {
        testState.domainsFetching = true;
      } else {
        testState.domainsStatus = status;
      }
      rerender(
        <NetworkAccessSection mcpServer={baseServer} endpoints={endpoints} />,
      );

      const confirm = screen.getByRole("button", {
        name: "Make private only",
      });
      expect(confirm.getAttribute("disabled")).not.toBeNull();
      fireEvent.click(confirm);
      expect(testState.mutate).not.toHaveBeenCalled();
    },
  );

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

  it("lets an eligible gateway use dual access and its private endpoint", async () => {
    const gatewayEndpoints = [{ ...endpoints[0], metaMcpServerId: gateway.id }];
    const { rerender } = render(
      <NetworkAccessSection
        metaMcpServer={gateway}
        endpoints={gatewayEndpoints}
      />,
    );

    fireEvent.click(
      screen.getByRole("combobox", { name: "Network access mode" }),
    );
    fireEvent.click(screen.getByRole("option", { name: /Public and private/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(testState.mutateGateway).toHaveBeenCalledWith({
      request: {
        updateMetaMcpServerForm: {
          id: gateway.id,
          name: gateway.name,
          networkAccessMode: "dual",
        },
      },
    });
    expect(testState.mutate).not.toHaveBeenCalled();

    await testState.gatewayMutationOptions?.onSuccess?.();
    expect(testState.invalidateGetGateway).toHaveBeenCalledWith(
      expect.anything(),
      { refetchType: "all" },
    );
    expect(testState.invalidateListGateway).toHaveBeenCalledWith(
      expect.anything(),
      { refetchType: "all" },
    );
    expect(testState.toastSuccess).toHaveBeenCalledWith(
      "Network access updated",
    );

    rerender(
      <NetworkAccessSection
        metaMcpServer={{ ...gateway, networkAccessMode: "dual" }}
        endpoints={gatewayEndpoints}
      />,
    );
    expect(
      screen.getByText("https://private.example.ts.net/mcp/hosted-mcp"),
    ).toBeTruthy();

    testState.gatewayMutationOptions?.onError?.(
      new Error("Gateway update failed"),
    );
    expect(testState.toastError).toHaveBeenCalledWith("Gateway update failed");
  });

  it("confirms private-only gateway access and lists affected URLs", () => {
    render(
      <NetworkAccessSection
        metaMcpServer={gateway}
        endpoints={[{ ...endpoints[0], metaMcpServerId: gateway.id }]}
      />,
    );

    fireEvent.click(
      screen.getByRole("combobox", { name: "Network access mode" }),
    );
    fireEvent.click(screen.getByRole("option", { name: /Private only/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    const dialog = screen.getByRole("dialog");
    expect(dialog.textContent).toContain(
      "Public routes stop serving this gateway",
    );
    expect(dialog.textContent).toContain(
      "https://platform.example.com/mcp/hosted-mcp",
    );
    expect(dialog.textContent).toContain(
      "https://private.example.ts.net/mcp/hosted-mcp",
    );
    expect(testState.mutateGateway).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Make private only" }));
    expect(testState.mutateGateway).toHaveBeenCalledWith({
      request: {
        updateMetaMcpServerForm: {
          id: gateway.id,
          name: gateway.name,
          networkAccessMode: "private_only",
        },
      },
    });
  });

  it("blocks gateway private access without an endpoint in the pinned namespace", () => {
    render(
      <NetworkAccessSection
        metaMcpServer={gateway}
        endpoints={[{ ...endpoints[1], metaMcpServerId: gateway.id }]}
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

  it("allows an existing private gateway to return to public-only access", () => {
    testState.entitled = false;
    render(
      <NetworkAccessSection
        metaMcpServer={{ ...gateway, networkAccessMode: "private_only" }}
        endpoints={[{ ...endpoints[0], metaMcpServerId: gateway.id }]}
      />,
    );
    expect(
      screen.getByText(/You can still switch to public only/),
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole("combobox", { name: "Network access mode" }),
    );
    fireEvent.click(screen.getByRole("option", { name: /Public only/ }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(testState.mutateGateway).toHaveBeenCalledWith({
      request: {
        updateMetaMcpServerForm: {
          id: gateway.id,
          name: gateway.name,
          networkAccessMode: "public_only",
        },
      },
    });
  });
});
