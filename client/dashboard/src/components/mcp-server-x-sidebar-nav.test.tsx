import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  McpServerCardStatus,
  RemoteIdentitySummary,
  SidebarUrlRow,
} from "./mcp-server-x-sidebar-nav";

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
});

describe("RemoteIdentitySummary", () => {
  function renderSummary(
    props: Partial<React.ComponentProps<typeof RemoteIdentitySummary>> = {},
  ) {
    return render(
      <MemoryRouter>
        <RemoteIdentitySummary
          mode={props.mode ?? "user"}
          passThroughAuthorization={props.passThroughAuthorization ?? false}
          authenticationRequired={props.authenticationRequired ?? false}
          unavailable={props.unavailable ?? false}
          loading={props.loading ?? false}
          settingsHref={
            props.settingsHref ?? "/mcp/x/example/settings#authentication"
          }
        />
      </MemoryRouter>,
    );
  }

  it.each([
    ["user", "User"],
    ["agent", "Agent"],
    ["none", "None"],
  ] as const)("makes the %s pill the way into settings", (mode, label) => {
    renderSummary({ mode });

    // The pill reports the setting and opens it; there is no separate link.
    const pill = screen.getByRole("link", { name: new RegExp(label) });
    expect(pill.getAttribute("href")).toBe(
      "/mcp/x/example/settings#authentication",
    );
    expect(screen.queryByRole("link", { name: "Setup" })).toBeNull();
  });

  it("hangs the explainer off the label, not the value", () => {
    renderSummary();

    // The question belongs to "Identity", not to whichever mode is set.
    expect(
      screen.getByRole("button", {
        name: "What do these identity modes mean?",
      }),
    ).toBeDefined();
  });

  it("keeps the problem out of the rail until hovered", () => {
    renderSummary({ mode: "none", authenticationRequired: true });

    // Warning states used to spend a line of the sidebar on their own; the
    // pill carries the amber and the detail waits for a hover.
    expect(screen.queryByText(/keep failing/i)).toBeNull();
    expect(screen.getByRole("link", { name: /None/ }).className).toContain(
      "border-warning-default",
    );
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
      expect(full?.className).toContain("whitespace-nowrap");
      expect(container.contains(full)).toBe(false);
    });
  });
});
