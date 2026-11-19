import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";

const mocks = vi.hoisted(() => ({
  probe: vi.fn(),
  capture: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  save: vi.fn(),
  canWriteMcp: true,
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
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope === "mcp:write" && mocks.canWriteMcp,
    isLoading: false,
  }),
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
// A native select stands in for the Radix one, which jsdom cannot open.
vi.mock("@/components/ui/Select", () => ({
  Select: ({
    value,
    onValueChange,
    children,
  }: {
    value: string;
    onValueChange: (value: string) => void;
    children: React.ReactNode;
  }) => (
    <select
      aria-label="Server to link"
      value={value}
      onChange={(event) => onValueChange(event.target.value)}
    >
      <option value="">Choose a server</option>
      {children}
    </select>
  ),
  SelectTrigger: () => null,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  SelectItem: ({
    value,
    children,
  }: {
    value: string;
    children: React.ReactNode;
  }) => <option value={value}>{children}</option>,
}));
vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
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
    mocks.toastError.mockReset();
    mocks.save.mockReset();
    mocks.save.mockResolvedValue(undefined);
    mocks.catalogServers = [];
    mocks.canWriteMcp = true;
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

  it("waits for the check before the catalog sheet can open", async () => {
    mocks.catalogServers = [
      {
        registrySpecifier: "com.example/tracker",
        title: "Tracker Cloud",
        remotes: [{ url: "https://tracker.example.com/mcp" }],
      },
    ];
    let answer: (value: unknown) => void = () => {};
    mocks.probe.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve;
      }),
    );
    renderPanel(server(), "/page?make-official=1");

    const add = await screen.findByRole("button", { name: "Add from catalog" });
    expect((add as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("dialog", { name: "Catalog sheet" })).toBeNull();

    answer({ outcome: "mcp_available" });
    expect(
      await screen.findByRole("dialog", { name: "Catalog sheet" }),
    ).toBeTruthy();
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

    await screen.findByRole("button", { name: "Check again" });
    // A check that did not finish is not a finding that the server needs a
    // manual setup.
    expect(screen.queryByText("This server needs a manual setup")).toBeNull();
    expect(screen.getByText(/The check did not finish/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Check again" }));

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
    expect(screen.queryByRole("switch")).toBeNull();
    expect(mocks.probe).not.toHaveBeenCalled();
  });

  it("removes the link and adds it back on undo", async () => {
    const official = {
      mcpServerId: "11111111-1111-4111-8111-111111111111",
      name: "Tracker",
      url: "https://app.example.com/mcp/tracker",
    };
    renderPanel(server({ officialServer: official }));

    expect(screen.queryByText("Link off")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove link" }));
    await vi.waitFor(() =>
      expect(mocks.save).toHaveBeenLastCalledWith({
        serverUrl: "https://tracker.example.com/mcp",
        mcpServerId: undefined,
      }),
    );

    await vi.waitFor(() => expect(mocks.toastSuccess).toHaveBeenCalled());
    const options = mocks.toastSuccess.mock.calls.at(-1)?.[1] as {
      action: { label: string; onClick: () => void };
    };
    expect(options.action.label).toBe("Undo");
    options.action.onClick();
    await vi.waitFor(() =>
      expect(mocks.save).toHaveBeenLastCalledWith({
        serverUrl: "https://tracker.example.com/mcp",
        mcpServerId: official.mcpServerId,
      }),
    );
    await vi.waitFor(() =>
      expect(mocks.toastSuccess).toHaveBeenLastCalledWith(
        "Link to “Tracker” restored",
      ),
    );
  });

  it("says so when undoing a removed link fails", async () => {
    const official = {
      mcpServerId: "11111111-1111-4111-8111-111111111111",
      name: "Tracker",
      url: "https://app.example.com/mcp/tracker",
    };
    renderPanel(server({ officialServer: official }));

    fireEvent.click(screen.getByRole("button", { name: "Remove link" }));
    await vi.waitFor(() => expect(mocks.toastSuccess).toHaveBeenCalled());
    const options = mocks.toastSuccess.mock.calls[0]?.[1] as {
      action: { label: string; onClick: () => void };
    };
    expect(options.action.label).toBe("Undo");

    mocks.save.mockRejectedValueOnce(new Error("nope"));
    options.action.onClick();
    await vi.waitFor(() =>
      expect(mocks.toastError).toHaveBeenCalledWith(
        "Could not restore the link. Try again.",
      ),
    );
    expect(mocks.save).toHaveBeenLastCalledWith({
      serverUrl: "https://tracker.example.com/mcp",
      mcpServerId: official.mcpServerId,
    });
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

  it("links the server picked under Advanced, never a disabled one", async () => {
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    renderPanel(server());
    await screen.findByRole("button", { name: "Make official" });

    fireEvent.click(screen.getByRole("button", { name: "Advanced" }));
    const select = screen.getByRole("combobox", { name: "Server to link" });
    expect(
      within(select)
        .getAllByRole("option")
        .map((option) => option.textContent),
    ).toEqual(["Choose a server", "Company Tools"]);

    fireEvent.change(select, { target: { value: "server-tools" } });
    fireEvent.click(screen.getByRole("button", { name: "Link" }));

    await waitFor(() => {
      expect(mocks.save).toHaveBeenCalledWith({
        serverUrl: "https://tracker.example.com/mcp",
        mcpServerId: "server-tools",
      });
    });
  });

  it("keeps adding a server off without permission to manage MCP servers", async () => {
    mocks.canWriteMcp = false;
    mocks.probe.mockResolvedValue({ outcome: "mcp_available" });
    renderPanel(server(), "/page?make-official=1");

    const make = await screen.findByRole("button", { name: "Make official" });
    expect((make as HTMLButtonElement).disabled).toBe(true);
    // The list's request to open the sheet is dropped rather than opening a
    // sheet whose save would be refused.
    expect(
      screen.queryByRole("dialog", { name: "Make official sheet" }),
    ).toBeNull();
  });

  it("starts over when the page moves to another server", async () => {
    mocks.probe.mockResolvedValue({
      outcome: "unreachable",
      reason: "timeout",
    });
    const { rerender } = renderPanel(server());
    fireEvent.click(
      await screen.findByRole("button", { name: "Request help" }),
    );
    expect(screen.getByRole("button", { name: "Help requested" })).toBeTruthy();

    rerender(
      <TooltipProvider>
        <MemoryRouter initialEntries={["/page"]}>
          <OfficialServerPanel
            server={server({
              canonicalServerUrl: "https://other.example.com/mcp",
              serverSlug: "other-example-com-mcp-87654321",
            })}
          />
        </MemoryRouter>
      </TooltipProvider>,
    );

    expect(
      await screen.findByRole("button", { name: "Request help" }),
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
