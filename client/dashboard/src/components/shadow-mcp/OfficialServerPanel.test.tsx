import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";

const mocks = vi.hoisted(() => ({
  probe: vi.fn(),
  capture: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("@gram/client/react-query/probeRemoteMcpURL.js", () => ({
  useProbeRemoteMcpURLMutation: () => ({
    mutateAsync: mocks.probe,
    isPending: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-1", slug: "acme" }),
  useProject: () => ({ id: "project-1", slug: "default" }),
}));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: mocks.capture }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      add: { remote: { href: () => "/acme/projects/default/mcp/add/remote" } },
      details: {
        overview: {
          href: (param: string) => `/acme/projects/default/mcp/${param}`,
        },
      },
    },
  }),
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: vi.fn() },
}));

import { OfficialServerPanel } from "./OfficialServerPanel";

function server(
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    access: "blocked",
    accessSummary: testAccessSummary("blocked"),
    allowedPolicyIds: [],
    blockedPolicyIds: [],
    canonicalServerUrl: "https://tracker.example.com/mcp",
    firstSeen: new Date("2026-01-01T10:00:00Z"),
    lastSeen: new Date("2026-01-02T10:00:00Z"),
    observedUseCount: 13,
    requestCount: 0,
    serverName: "Tracker",
    serverSlug: "tracker-example-com-mcp-12345678",
    topUsers: [],
    urlHost: "tracker.example.com",
    userCount: 5,
    ...overrides,
  };
}

function renderPanel(target: ShadowMCPInventoryServer) {
  return render(
    <TooltipProvider>
      <MemoryRouter>
        <OfficialServerPanel server={target} />
      </MemoryRouter>
    </TooltipProvider>,
  );
}

describe("OfficialServerPanel", () => {
  beforeEach(() => {
    mocks.probe.mockReset();
    mocks.capture.mockReset();
    mocks.toastSuccess.mockReset();
    window.localStorage.clear();
  });
  afterEach(cleanup);

  it("offers to make a server official once every check passes", async () => {
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    renderPanel(server());

    const link = await screen.findByRole("link", { name: "Make official" });
    const href = new URL(
      link.getAttribute("href") ?? "",
      "https://app.example.com",
    );
    expect(href.pathname).toBe("/acme/projects/default/mcp/add/remote");
    expect(href.searchParams.get("url")).toBe(
      "https://tracker.example.com/mcp",
    );
    expect(href.searchParams.get("name")).toBe("Tracker");

    expect(mocks.probe).toHaveBeenCalledWith({
      request: { probeURLForm: { url: "https://tracker.example.com/mcp" } },
    });
    expect(screen.getByText("It needs no sign-in.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Request help" })).toBeNull();
  });

  it("says what is in the way and takes a request for help", async () => {
    mocks.probe.mockResolvedValue({
      outcome: "unreachable",
      reason: "guardian_rejected",
    });
    renderPanel(server());

    expect(
      await screen.findByText(
        "It is on a private network that Speakeasy cannot reach.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Make official" })).toBeNull();
    // The admin can still try the form with a credential of their own.
    expect(screen.getByRole("link", { name: "Set it up myself" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Request help" }));

    expect(mocks.capture).toHaveBeenCalledWith(
      "shadow_mcp_onboarding_requested",
      {
        organization_slug: "acme",
        project_slug: "default",
        server_url: "https://tracker.example.com/mcp",
        server_name: "Tracker",
        blocker: "unreachable:guardian_rejected",
        people_using_it: 5,
        calls: 13,
      },
    );
    expect(mocks.toastSuccess).toHaveBeenCalled();
    const requested = screen.getByRole("button", { name: "Help requested" });
    expect((requested as HTMLButtonElement).disabled).toBe(true);
  });

  it("lets the admin run the check again when it fails", async () => {
    mocks.probe.mockRejectedValueOnce(new Error("boom"));
    mocks.probe.mockResolvedValueOnce({ outcome: "mcp_available" });
    renderPanel(server());

    fireEvent.click(await screen.findByRole("button", { name: "Check again" }));

    expect(
      await screen.findByRole("link", { name: "Make official" }),
    ).toBeTruthy();
    expect(mocks.probe).toHaveBeenCalledTimes(2);
  });

  it("shows where an onboarded server is served and runs no check", () => {
    renderPanel(
      server({
        officialServer: {
          mcpServerId: "11111111-1111-4111-8111-111111111111",
          name: "Tracker",
          slug: "tracker",
          url: "https://app.example.com/mcp/team-gateway",
          gatewayName: "Team Gateway",
        },
      }),
    );

    expect(screen.getByText("Onboarded")).toBeTruthy();
    expect(
      screen.getByText(
        "Served as “Tracker” through the gateway “Team Gateway”. Direct connections are blocked and agents are sent there.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("https://app.example.com/mcp/team-gateway"),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Open official server" })
        .getAttribute("href"),
    ).toBe("/acme/projects/default/mcp/tracker");
    expect(mocks.probe).not.toHaveBeenCalled();
  });

  it("renders nothing for a local command", () => {
    const { container } = renderPanel(
      server({
        targetKind: "stdio_command",
        canonicalServerUrl: "npx -y example-mcp",
      }),
    );

    expect(container.innerHTML).toBe("");
    expect(mocks.probe).not.toHaveBeenCalled();
  });
});
