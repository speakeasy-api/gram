import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { CatalogServer } from "./hooks";
import { TeamUsageTable } from "./TeamUsageTable";

const mocks = vi.hoisted(() => ({ navigate: vi.fn() }));

vi.mock("react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("react-router")>()),
  useNavigate: () => mocks.navigate,
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      add: { remote: { href: () => "/mcp/add/remote" } },
      details: { overview: { href: (id: string) => `/mcp/${id}` } },
    },
    shadowMCP: { href: () => "/shadow-mcp" },
  }),
}));

function server(
  overrides: Partial<ShadowMCPInventoryServer>,
): ShadowMCPInventoryServer {
  return {
    canonicalServerUrl: "https://mcp.example.com/mcp",
    serverSlug: "mcp-example-com",
    urlHost: "mcp.example.com",
    firstSeen: "2026-11-01T00:00:00Z",
    lastSeen: "2026-11-18T00:00:00Z",
    observedUseCount: 12,
    userCount: 3,
    topUsers: [],
    access: "none",
    requestCount: 0,
    allowedPolicyIds: [],
    blockedPolicyIds: [],
    ...overrides,
  } as ShadowMCPInventoryServer;
}

const catalogEntry = {
  registrySpecifier: "app.linear/linear",
  remotes: [{ url: "https://mcp.linear.app/mcp" }],
} as unknown as CatalogServer;

function renderTable(
  servers: ShadowMCPInventoryServer[],
  onAddFromCatalog = vi.fn(),
  { search = "", catalogLoading = false } = {},
) {
  render(
    <MemoryRouter>
      <TeamUsageTable
        servers={servers}
        catalog={[catalogEntry]}
        catalogLoading={catalogLoading}
        search={search}
        isLoading={false}
        isError={false}
        onRetry={() => undefined}
        onAddFromCatalog={(entry) => {
          onAddFromCatalog(entry);
        }}
      />
    </MemoryRouter>,
  );
  return { onAddFromCatalog };
}

afterEach(() => {
  cleanup();
  mocks.navigate.mockReset();
});

describe("TeamUsageTable", () => {
  it("opens the remote create form with the URL filled in", () => {
    renderTable([server({ serverName: "Internal tools" })]);
    fireEvent.click(screen.getByRole("button", { name: "Add Internal tools" }));
    const href = mocks.navigate.mock.calls[0]?.[0] as string;
    const params = new URL(href, "https://app.test").searchParams;
    expect(href.startsWith("/mcp/add/remote?")).toBe(true);
    expect(params.get("url")).toBe("https://mcp.example.com/mcp");
    expect(params.get("name")).toBe("Internal tools");
  });

  it("uses the catalog entry when one serves the URL", () => {
    const { onAddFromCatalog } = renderTable([
      server({
        serverName: "Linear",
        canonicalServerUrl: "https://mcp.linear.app/mcp",
      }),
    ]);
    fireEvent.click(screen.getByRole("button", { name: "Add Linear" }));
    expect(onAddFromCatalog).toHaveBeenCalledWith(catalogEntry);
    expect(mocks.navigate).not.toHaveBeenCalled();
  });

  it("links to the project server that already serves the URL", () => {
    renderTable([
      server({
        serverName: "Notion",
        suggestedOfficialServer: {
          mcpServerId: "srv-1",
          name: "Notion",
          slug: "notion",
        },
      }),
    ]);
    expect(screen.queryByRole("button", { name: "Add Notion" })).toBeNull();
    expect(
      screen.getByRole("link", { name: "Open" }).getAttribute("href"),
    ).toBe("/mcp/notion");
  });

  it("names the next step when nothing is used yet", () => {
    renderTable([]);
    expect(screen.getByText("No servers to suggest yet")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Open Shadow MCP" })
        .getAttribute("href"),
    ).toBe("/shadow-mcp");
  });

  it("filters by name, host or URL and says when nothing matches", () => {
    const servers = [
      server({
        serverName: "Linear",
        canonicalServerUrl: "https://mcp.linear.app/mcp",
        urlHost: "mcp.linear.app",
      }),
      server({ serverName: "Internal tools" }),
    ];
    renderTable(servers, vi.fn(), { search: "linear" });
    expect(screen.getByText("Linear")).toBeTruthy();
    expect(screen.queryByText("Internal tools")).toBeNull();
    cleanup();
    renderTable(servers, vi.fn(), { search: "nothing-like-this" });
    expect(
      screen.getByText("No servers matching “nothing-like-this”"),
    ).toBeTruthy();
  });

  it("holds Add until the catalog has loaded", () => {
    renderTable(
      [
        server({
          serverName: "Linear",
          canonicalServerUrl: "https://mcp.linear.app/mcp",
        }),
      ],
      vi.fn(),
      { catalogLoading: true },
    );
    const add = screen.getByRole("button", { name: "Add Linear" });
    expect((add as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(add);
    expect(mocks.navigate).not.toHaveBeenCalled();
  });
});
