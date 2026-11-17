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
vi.mock("./CatalogOfficialSheet", () => ({
  CatalogOfficialSheet: ({
    open,
    entry,
  }: {
    open: boolean;
    entry: { registrySpecifier: string };
  }) =>
    open ? (
      <div role="dialog" aria-label="Catalog sheet">
        {entry.registrySpecifier}
      </div>
    ) : null,
}));
vi.mock("./MakeOfficialSheet", () => ({
  MakeOfficialSheet: ({ open }: { open: boolean }) =>
    open ? <div role="dialog" aria-label="Make official sheet" /> : null,
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

function renderPanel(target: ShadowMCPInventoryServer, path = "/page") {
  return render(
    <TooltipProvider>
      <MemoryRouter initialEntries={[path]}>
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

    const button = await screen.findByRole("button", { name: "Make official" });
    expect(mocks.probe).toHaveBeenCalledWith({
      request: { probeURLForm: { url: "https://tracker.example.com/mcp" } },
    });
    expect(screen.getByText("It needs no sign-in.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Request help" })).toBeNull();

    // The form opens in a sheet on this page, not on another page.
    expect(
      screen.queryByRole("dialog", { name: "Make official sheet" }),
    ).toBeNull();
    fireEvent.click(button);
    expect(
      screen.getByRole("dialog", { name: "Make official sheet" }),
    ).toBeTruthy();
  });

  it("keeps the sheet open on its result once the server is linked", async () => {
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    const view = renderPanel(server());
    fireEvent.click(
      await screen.findByRole("button", { name: "Make official" }),
    );

    // Linking refreshes the server: the panel shows the link, and the sheet
    // stays where it is.
    view.rerender(
      <TooltipProvider>
        <MemoryRouter initialEntries={["/page"]}>
          <OfficialServerPanel
            server={server({
              officialServer: {
                mcpServerId: "11111111-1111-4111-8111-111111111111",
                name: "Tracker",
                directAccessAllowed: false,
              },
            })}
          />
        </MemoryRouter>
      </TooltipProvider>,
    );
    expect(screen.getByText("Onboarded")).toBeTruthy();
    expect(
      screen.getByRole("dialog", { name: "Make official sheet" }),
    ).toBeTruthy();
  });

  it("opens the sheet when the list asked for it and the checks pass", async () => {
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    renderPanel(server(), "/page?make-official=1");

    expect(
      await screen.findByRole("dialog", { name: "Make official sheet" }),
    ).toBeTruthy();
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
    expect(screen.queryByRole("button", { name: "Make official" })).toBeNull();
    // The admin can still try the form with a credential of their own.
    expect(
      screen.getByRole("button", { name: "Set it up myself" }),
    ).toBeTruthy();

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

  it("adds a manual sign-in server from the catalog in a sheet", async () => {
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
      await screen.findByText("In the Speakeasy catalog as “Tracker Cloud”"),
    ).toBeTruthy();
    // The catalog stands in for the manual-setup warning, and nothing
    // leaves the page.
    expect(screen.queryByText("This server needs a manual setup")).toBeNull();
    expect(screen.queryByRole("link")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Add from catalog" }));
    expect(
      screen.getByRole("dialog", { name: "Catalog sheet" }).textContent,
    ).toBe("com.example/tracker");
  });

  it("prefers the catalog version even when a custom server would work", async () => {
    mocks.catalogServers = [
      {
        registrySpecifier: "com.example/tracker",
        title: "Tracker Cloud",
        remotes: [{ url: "https://tracker.example.com/mcp" }],
      },
    ];
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    renderPanel(server());

    expect(
      await screen.findByRole("button", { name: "Add from catalog" }),
    ).toBeTruthy();
    expect(screen.getByText("Recommended")).toBeTruthy();
    // A new server is still one step away, under Advanced.
    expect(screen.queryByRole("button", { name: "Make official" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    fireEvent.click(screen.getByRole("button", { name: "Make official" }));
    expect(
      screen.getByRole("dialog", { name: "Make official sheet" }),
    ).toBeTruthy();
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
      await screen.findByRole("button", { name: "Make official" }),
    ).toBeTruthy();
    expect(mocks.probe).toHaveBeenCalledTimes(2);
  });

  it("shows the linked server and runs no check", () => {
    renderPanel(
      server({
        officialServer: {
          mcpServerId: "11111111-1111-4111-8111-111111111111",
          name: "Tracker",
          slug: "tracker",
          url: "https://app.example.com/mcp/team-gateway",
          gatewayName: "Team Gateway",
          directAccessAllowed: false,
        },
      }),
    );

    expect(screen.getByText("Onboarded")).toBeTruthy();
    expect(
      screen.getByText(
        "Linked to “Tracker”, served through the gateway “Team Gateway”. Direct connections are blocked and agents are sent there.",
      ),
    ).toBeTruthy();
    expect(
      screen.getByText("https://app.example.com/mcp/team-gateway"),
    ).toBeTruthy();
    const open = screen.getByRole("link", { name: "Open" });
    expect(open.getAttribute("href")).toBe(
      "/acme/projects/default/mcp/tracker",
    );
    expect(open.getAttribute("target")).toBe("_blank");
    expect(
      screen
        .getByRole("switch", { name: "Linked" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(mocks.probe).not.toHaveBeenCalled();
  });

  it("switches the link off and on, and removes it", async () => {
    const official = {
      mcpServerId: "11111111-1111-4111-8111-111111111111",
      name: "Tracker",
      url: "https://app.example.com/mcp/tracker",
      directAccessAllowed: false,
    };
    const view = renderPanel(server({ officialServer: official }));

    fireEvent.click(screen.getByRole("switch", { name: "Linked" }));
    await vi.waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith({
        serverUrl: "https://tracker.example.com/mcp",
        mcpServerId: official.mcpServerId,
        allowDirectAccess: true,
      }),
    );

    view.unmount();
    renderPanel(
      server({ officialServer: { ...official, directAccessAllowed: true } }),
    );
    expect(screen.getByText("Link off")).toBeTruthy();
    fireEvent.click(screen.getByRole("switch", { name: "Linked" }));
    await vi.waitFor(() =>
      expect(mocks.save).toHaveBeenLastCalledWith({
        serverUrl: "https://tracker.example.com/mcp",
        mcpServerId: official.mcpServerId,
        allowDirectAccess: false,
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Remove link" }));
    await vi.waitFor(() =>
      expect(mocks.save).toHaveBeenLastCalledWith({
        serverUrl: "https://tracker.example.com/mcp",
        mcpServerId: undefined,
        allowDirectAccess: false,
      }),
    );
  });

  it("recommends the server that already serves the URL, others under Advanced", async () => {
    mocks.catalogServers = [
      {
        registrySpecifier: "com.example/tracker",
        title: "Tracker Cloud",
        remotes: [{ url: "https://tracker.example.com/mcp" }],
      },
    ];
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    renderPanel(
      server({
        suggestedOfficialServer: {
          mcpServerId: "22222222-2222-4222-8222-222222222222",
          name: "Tracker Cloud",
          slug: "tracker-cloud",
        },
      }),
    );

    expect(screen.getByText("Already served as “Tracker Cloud”")).toBeTruthy();
    expect(screen.getByText("Recommended")).toBeTruthy();
    // The catalog and a new server stay one step away.
    expect(
      screen.queryByRole("button", { name: "Add from catalog" }),
    ).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    expect(
      await screen.findByRole("button", { name: "Add from catalog" }),
    ).toBeTruthy();
    expect(screen.getByRole("button", { name: "Make official" })).toBeTruthy();
    expect(screen.getByText("Link another server")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Link it" }));
    await vi.waitFor(() =>
      expect(mocks.save).toHaveBeenCalledWith({
        serverUrl: "https://tracker.example.com/mcp",
        mcpServerId: "22222222-2222-4222-8222-222222222222",
        allowDirectAccess: false,
      }),
    );
  });

  it("links another server from the closed Advanced section", async () => {
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    renderPanel(server());
    await screen.findByRole("button", { name: "Make official" });

    expect(screen.queryByText("Link another server")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    expect(screen.getByText("Link another server")).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: "Link" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
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
