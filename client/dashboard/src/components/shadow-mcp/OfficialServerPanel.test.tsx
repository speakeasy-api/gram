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
  save: vi.fn(),
  catalogServers: [] as Array<{
    registrySpecifier: string;
    title?: string;
    remotes: Array<{ url: string }>;
  }>,
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
      catalog: {
        detail: {
          href: (specifier: string) =>
            `/acme/projects/default/mcp/catalog/${specifier}`,
        },
      },
      details: {
        overview: {
          href: (param: string) => `/acme/projects/default/mcp/${param}`,
        },
      },
    },
  }),
}));
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: {
      mcpServers: [
        { id: "server-tools", name: "Company Tools", visibility: "private" },
        { id: "server-off", name: "Old Server", visibility: "disabled" },
      ],
    },
  }),
}));
vi.mock("@/pages/catalog/hooks", () => ({
  useListMCPCatalog: () => ({
    data: { servers: mocks.catalogServers },
    isLoading: false,
  }),
}));
vi.mock("./useSaveShadowMCPOfficialServer", () => ({
  useSaveShadowMCPOfficialServer: () => ({
    save: mocks.save,
    isPending: false,
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
    mocks.save.mockReset();
    mocks.save.mockResolvedValue(undefined);
    mocks.catalogServers = [];
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
        catalog_specifier: null,
        people_using_it: 5,
        calls: 13,
      },
    );
    expect(mocks.toastSuccess).toHaveBeenCalled();
    const requested = screen.getByRole("button", { name: "Help requested" });
    expect((requested as HTMLButtonElement).disabled).toBe(true);
  });

  it("sends a manual sign-in to the catalog entry for the same URL", async () => {
    mocks.catalogServers = [
      {
        registrySpecifier: "com.example/tracker",
        title: "Tracker Cloud",
        remotes: [{ url: "https://tracker.example.com/mcp/" }],
      },
    ];
    mocks.probe.mockResolvedValue({
      outcome: "authentication_required",
      protectedResourceMetadataUrl:
        "https://tracker.example.com/.well-known/oauth-protected-resource",
      signInSetup: "manual",
    });
    renderPanel(server());

    expect(
      await screen.findByText("Set it up from the Speakeasy catalog"),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Set up from the catalog" })
        .getAttribute("href"),
    ).toBe("/acme/projects/default/mcp/catalog/com.example%2Ftracker");
    expect(screen.queryByText("This server needs a manual setup")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Request help" }));
    expect(mocks.capture).toHaveBeenCalledWith(
      "shadow_mcp_onboarding_requested",
      expect.objectContaining({
        blocker: "sign_in_manual",
        catalog_specifier: "com.example/tracker",
      }),
    );
  });

  it("asks for a manual setup when the catalog has no entry", async () => {
    mocks.catalogServers = [
      {
        registrySpecifier: "com.example/other",
        remotes: [{ url: "https://other.example.com/mcp" }],
      },
    ];
    mocks.probe.mockResolvedValue({
      outcome: "authentication_required",
      protectedResourceMetadataUrl:
        "https://tracker.example.com/.well-known/oauth-protected-resource",
      signInSetup: "manual",
    });
    renderPanel(server());

    expect(
      await screen.findByText("This server needs a manual setup"),
    ).toBeTruthy();
    expect(
      screen.queryByRole("link", { name: "Set up from the catalog" }),
    ).toBeNull();
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
          chosen: false,
          directAccessAllowed: false,
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

  it("keeps direct connections working from the closed Advanced section", async () => {
    renderPanel(
      server({
        officialServer: {
          mcpServerId: "11111111-1111-4111-8111-111111111111",
          name: "Tracker",
          url: "https://app.example.com/mcp/tracker",
          chosen: false,
          directAccessAllowed: false,
        },
      }),
    );

    // Closed until asked for.
    expect(screen.queryByText("Keep direct connections working")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));

    const save = screen.getByRole("button", { name: "Save" });
    expect((save as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Keep direct connections working/ }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await vi.waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith({
        serverUrl: "https://tracker.example.com/mcp",
        mcpServerId: undefined,
        allowDirectAccess: true,
      }),
    );
    expect(mocks.toastSuccess).toHaveBeenCalled();
  });

  it("cannot keep direct connections before there is an official server", async () => {
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    renderPanel(server());
    await screen.findByRole("link", { name: "Make official" });

    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    const keep = screen.getByRole("checkbox", {
      name: /Keep direct connections working/,
    });
    expect(keep.hasAttribute("disabled")).toBe(true);
    expect(
      screen.getByText("Available once this server has an official version."),
    ).toBeTruthy();
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
