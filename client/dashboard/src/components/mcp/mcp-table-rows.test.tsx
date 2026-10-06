import { TooltipProvider } from "@/components/ui/Tooltip";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, expect, it, vi } from "vitest";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";

const { resolveEndpoints } = vi.hoisted(() => ({
  resolveEndpoints: vi.fn((endpoints: McpEndpoint[]) => {
    const endpoint = endpoints.find((candidate) => !candidate.customDomainId);
    const mcpUrl = endpoint
      ? `https://gram.example/mcp/${endpoint.slug}`
      : undefined;
    return {
      mcpUrl,
      installPageUrl: mcpUrl ? `${mcpUrl}/install` : undefined,
      loading: false,
    };
  }),
}));

vi.mock("@/hooks/useToolsetUrl", () => ({
  useMcpUrl: () => ({ url: undefined }),
  useResolvedMcpServerUrl: resolveEndpoints,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { gateway: { overview: { href: (id: string) => `/mcp/g/${id}` } } },
  }),
}));

import { GatewayTableRow } from "./mcp-table-rows";

afterEach(cleanup);

const gateway = {
  id: "gateway-id",
  name: "Engineering",
  memberCount: 3,
} as MetaMcpServer;

function renderRow(endpoints: McpEndpoint[]) {
  return render(
    <MemoryRouter>
      <TooltipProvider>
        <table>
          <tbody>
            <GatewayTableRow
              gateway={gateway}
              endpoints={endpoints}
              isLoadingEndpoints={false}
            />
          </tbody>
        </table>
      </TooltipProvider>
    </MemoryRouter>,
  );
}

it("renders a gateway endpoint address and copy action", () => {
  const endpoint = {
    id: "endpoint-id",
    metaMcpServerId: gateway.id,
    slug: "org-gateway",
  } as McpEndpoint;

  renderRow([endpoint]);

  expect(resolveEndpoints).toHaveBeenCalledWith([endpoint], false);
  expect(screen.getByText("gram.example/mcp/org-gateway")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Copy MCP URL" })).toBeTruthy();
});

it("does not invent a Gram address for a custom-domain-only endpoint", () => {
  renderRow([
    {
      id: "custom-endpoint-id",
      metaMcpServerId: gateway.id,
      slug: "custom-gateway",
      customDomainId: "custom-domain-id",
    } as McpEndpoint,
  ]);

  expect(screen.getByText("—")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Copy MCP URL" })).toBeNull();
});
