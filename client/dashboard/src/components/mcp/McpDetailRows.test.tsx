import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { McpServerCardStatus, SidebarUrlRow } from "./McpDetailRows";

vi.mock("@/pages/mcp/x/MCPServerDetails", () => ({
  default: () => null,
  MCPServerAvailabilityToggle: () => (
    <button role="switch" aria-label="Server availability" />
  ),
  MCPServerStatusDropdown: () => <button>Private</button>,
}));

function server(overrides: Partial<McpServer>): McpServer {
  return {
    id: "mcp-server-1",
    projectId: "project-1",
    networkAccessMode: "public_only",
    visibility: "private",
    createdAt: new Date(0),
    updatedAt: new Date(0),
    ...overrides,
  } as McpServer;
}

afterEach(cleanup);

describe("McpServerCardStatus", () => {
  it("renders an unlabeled availability switch for Remote MCP", () => {
    render(
      <McpServerCardStatus
        server={server({ remoteMcpServerId: "remote-source-1" })}
      />,
    );

    expect(
      screen.getByRole("switch", { name: "Server availability" }),
    ).toBeDefined();
    expect(screen.queryByText("Visibility")).toBeNull();
    expect(screen.queryByRole("button", { name: "Private" })).toBeNull();
  });

  it("preserves the labeled visibility dropdown for tunneled MCP", () => {
    render(
      <McpServerCardStatus
        server={server({ tunneledMcpServerId: "tunneled-source-1" })}
      />,
    );

    expect(screen.getByText("Visibility")).toBeDefined();
    expect(screen.getByRole("button", { name: "Private" })).toBeDefined();
    expect(screen.queryByRole("switch")).toBeNull();
  });

  it("renders nothing for unproxied MCP", () => {
    const { container } = render(
      <McpServerCardStatus
        server={server({ unproxiedMcpServerId: "unproxied-source-1" })}
      />,
    );

    expect(container.childElementCount).toBe(0);
  });
});

describe("SidebarUrlRow", () => {
  it("clips to one line and reveals the whole URL on hover", async () => {
    const url = "https://localhost:34183/mcp/speakeasy-a-very-long-server-slug";
    const display = "localhost:34183/mcp/speakeasy-a-very-long-server-slug";
    const { container } = render(
      <MemoryRouter>
        {/* The app mounts one of these at the root; CopyButton needs it. */}
        <TooltipProvider>
          <SidebarUrlRow label="URL" url={url} copyTooltip="Copy URL" />
        </TooltipProvider>
      </MemoryRouter>,
    );

    const line = container.querySelector(
      '[data-slot="sidebar-url-line"]',
    ) as HTMLElement;
    expect(line.textContent).toBe(display);
    expect(line.className).toContain("truncate");
    expect(document.querySelector('[data-slot="sidebar-url-full"]')).toBeNull();

    await userEvent.hover(line);

    // Portalled, because the rail scrolls and would clip anything that tried
    // to escape it from inside.
    await waitFor(() => {
      const full = document.querySelector(
        '[data-slot="sidebar-url-full"]',
      ) as HTMLElement | null;
      expect(full).not.toBeNull();
      expect(full?.textContent).toBe(display);
      // Bounded to the viewport, and long URLs break instead of spilling off it.
      expect(full?.className).toContain("max-w-[calc(100vw-2rem)]");
      expect(full?.className).toContain("break-all");
      expect(container.contains(full)).toBe(false);
    });
  });
});
