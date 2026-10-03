import { TooltipProvider } from "@/components/ui/Tooltip";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { expect, it, vi } from "vitest";

const { resolveEndpoint } = vi.hoisted(() => ({
  resolveEndpoint: vi.fn(() => ({
    mcpUrl: "https://gram.example/mcp/org-gateway",
    installPageUrl: "https://gram.example/mcp/org-gateway/install",
  })),
}));
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";

vi.mock("@/hooks/useToolsetUrl", () => ({
  useMcpUrl: () => ({ url: undefined }),
  useMcpEndpointUrl: resolveEndpoint,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: { gateway: { overview: { href: (id: string) => `/mcp/g/${id}` } } },
  }),
}));

import { GatewayTableRow } from "./mcp-table-rows";

it("renders a gateway endpoint address and copy action", () => {
  const gateway = {
    id: "gateway-id",
    name: "Engineering",
    memberCount: 3,
  } as MetaMcpServer;
  const endpoint = {
    id: "endpoint-id",
    metaMcpServerId: gateway.id,
    slug: "org-gateway",
  } as McpEndpoint;

  render(
    <MemoryRouter>
      <TooltipProvider>
        <table>
          <tbody>
            <GatewayTableRow gateway={gateway} endpoints={[endpoint]} />
          </tbody>
        </table>
      </TooltipProvider>
    </MemoryRouter>,
  );

  expect(resolveEndpoint).toHaveBeenCalledWith(endpoint);
  expect(screen.getByText("gram.example/mcp/org-gateway")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Copy MCP URL" })).toBeTruthy();
});
