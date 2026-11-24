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
  useResolvedMcpServerUrl: resolveEndpoints,
}));

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import { GatewayTableRow, ProjectGatewayTableRow } from "./mcp-table-rows";

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

it("links the row to the gateway overview", () => {
  renderRow([]);

  expect(
    screen
      .getByRole("link", { name: "Open gateway Engineering" })
      .getAttribute("href"),
  ).toBe("/org/projects/project/mcp/gateway/gateway-id/overview");
});

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

it("does not invent a Speakeasy address for a custom-domain-only endpoint", () => {
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

function renderProjectGatewayRow(
  status: Parameters<typeof ProjectGatewayTableRow>[0]["status"],
  url?: string,
) {
  return render(
    <MemoryRouter>
      <TooltipProvider>
        <table>
          <tbody>
            <ProjectGatewayTableRow status={status} url={url} />
          </tbody>
        </table>
      </TooltipProvider>
    </MemoryRouter>,
  );
}

it("renders the Project Gateway row with its URL when available", () => {
  renderProjectGatewayRow(
    "available",
    "https://gram.example/mcp/gateway/org/projects/project",
  );

  expect(
    screen
      .getByRole("link", { name: "Open Project Gateway" })
      .getAttribute("href"),
  ).toBe("/org/projects/project/mcp/project-gateway");
  expect(
    screen.getByText("gram.example/mcp/gateway/org/projects/project"),
  ).toBeTruthy();
  expect(screen.queryByText("Off")).toBeNull();
  expect(screen.queryByText("Unavailable")).toBeNull();
});

it("renders the Project Gateway row in a disabled state when off", () => {
  renderProjectGatewayRow("off");

  expect(
    screen
      .getByRole("link", { name: "Open Project Gateway" })
      .getAttribute("href"),
  ).toBe("/org/projects/project/mcp/project-gateway");
  expect(screen.getByText("Off")).toBeTruthy();
  expect(screen.getByText("—")).toBeTruthy();
});

it("renders the Project Gateway row in a disabled state when unavailable", () => {
  renderProjectGatewayRow("unavailable");

  expect(
    screen
      .getByRole("link", { name: "Open Project Gateway" })
      .getAttribute("href"),
  ).toBe("/org/projects/project/mcp/project-gateway");
  expect(screen.getByText("Unavailable")).toBeTruthy();
  expect(screen.getByText("—")).toBeTruthy();
});
