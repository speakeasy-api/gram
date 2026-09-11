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
  featureStatus: "success" as "pending" | "success" | "error",
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
  mutationOptions: undefined as
    | {
        onError?: (error: Error) => void;
        onSuccess?: () => Promise<void>;
      }
    | undefined,
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
  testState.featureStatus = "success";
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
});
