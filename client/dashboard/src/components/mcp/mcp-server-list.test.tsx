import { TooltipProvider } from "@/components/ui/Tooltip";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  oauthStatus: "not-external-mcp" as string,
}));

vi.mock("@/components/sources/sources-hooks", () => ({
  useHostedMcpOAuthConfigStatus: () => mocks.oauthStatus,
}));
vi.mock("@/components/sources/SourceCard", () => ({
  SourceMcpIcon: () => null,
}));
vi.mock("./MCPToolsetIcon", () => ({
  ExternalMcpIcon: () => null,
}));
vi.mock("@/components/icon-dither", () => ({
  useIconDither: () => ({
    canvasRef: { current: null },
    start: vi.fn(),
    stop: vi.fn(),
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({
    mcp: {
      details: {
        href: (slug: string) => `/mcp/${slug}`,
        authentication: {
          href: (slug: string) => `/mcp/${slug}/authentication`,
        },
      },
      x: {
        overview: { href: (slug: string) => `/mcp/x/${slug}` },
        settings: { href: (slug: string) => `/mcp/x/${slug}/settings` },
      },
    },
  }),
}));

import { MCPServerCard } from "./MCPServerCard";
import { MCPServerTableRow } from "./mcp-table-rows";
import { listedMcpServers } from "./mcp-server-kind";

afterEach(cleanup);
beforeEach(() => {
  mocks.oauthStatus = "not-external-mcp";
});

function server(fields: Partial<McpServer>): McpServer {
  return {
    id: "server-1",
    name: "Server",
    slug: "server-1",
    visibility: "private",
    ...fields,
  } as McpServer;
}

const toolset = {
  id: "toolset-1",
  name: "Hosted tools",
  slug: "hosted-tools",
  toolCount: 12,
  toolSelectionMode: "static",
  externalMcpSlugs: [],
  externalMcpProxyOnly: false,
};

const HOSTED = server({
  id: "toolset-1",
  name: "Hosted wrapper",
  slug: "hosted-wrapper",
  toolsetId: "toolset-1",
  toolset,
  primaryEndpointUrl: "https://mcp.example.com/mcp/hosted",
});
const REMOTE = server({
  id: "remote-row",
  name: "Docs search",
  slug: "remote-a1",
  remoteMcpServerId: "remote-1",
  primaryEndpointUrl: "https://app.example.com/mcp/remote-a1",
});
const TUNNELED = server({
  id: "tunneled-row",
  name: "Warehouse",
  slug: "tunneled-b2",
  tunneledMcpServerId: "tunnel-1",
});
const UNPROXIED = server({
  id: "unproxied-row",
  name: "Local files",
  slug: "unproxied-c3",
  unproxiedMcpServerId: "unproxied-1",
});

function renderRows(rows: McpServer[]) {
  return render(
    <MemoryRouter>
      <TooltipProvider>
        <table>
          <tbody>
            {listedMcpServers(rows).map(({ server, kind }) => (
              <MCPServerTableRow key={server.id} server={server} kind={kind} />
            ))}
          </tbody>
        </table>
      </TooltipProvider>
    </MemoryRouter>,
  );
}

function renderCards(rows: McpServer[]) {
  return render(
    <MemoryRouter>
      <TooltipProvider>
        {listedMcpServers(rows).map(({ server, kind }) => (
          <MCPServerCard key={server.id} server={server} kind={kind} />
        ))}
      </TooltipProvider>
    </MemoryRouter>,
  );
}

describe("listedMcpServers", () => {
  it("keeps every backed row and drops rows with no backend", () => {
    expect(
      listedMcpServers([
        HOSTED,
        REMOTE,
        TUNNELED,
        UNPROXIED,
        server({ id: "backendless" }),
      ]).map(({ server, kind }) => [server.id, kind]),
    ).toEqual([
      ["toolset-1", "hosted"],
      ["remote-row", "remote"],
      ["tunneled-row", "tunneled"],
      ["unproxied-row", "unproxied"],
    ]);
  });
});

describe("hosted wrappers", () => {
  const extra = (id: string, name: string, hasEndpoint?: boolean) =>
    server({
      id,
      name,
      slug: id,
      toolsetId: "toolset-1",
      toolset,
      hasEndpoint,
    });

  it("hides a non-canonical wrapper with no endpoint of its own", () => {
    renderRows([extra("wrapper-2", "Support (gateway)", false), HOSTED]);
    expect(
      screen.getAllByRole("link").map((link) => link.getAttribute("href")),
    ).toEqual(["/mcp/x/hosted-wrapper"]);
  });

  it("hides a toolset's only wrapper when it has no endpoint", () => {
    renderRows([extra("wrapper-2", "Support (gateway)", false), REMOTE]);
    expect(screen.queryByText("Support (gateway)")).toBeNull();
  });

  it("lists a non-canonical wrapper with its own endpoint as its own server", () => {
    renderRows([HOSTED, extra("wrapper-2", "Support (staging)", true)]);
    expect(
      screen.getAllByRole("link").map((link) => link.getAttribute("href")),
    ).toEqual(["/mcp/x/hosted-wrapper", "/mcp/x/wrapper-2"]);
    expect(screen.getByText("Support (staging)")).toBeTruthy();
  });

  it("lists a non-canonical wrapper when has_endpoint could not be read", () => {
    renderRows([HOSTED, extra("wrapper-2", "Support (unknown)")]);
    expect(screen.getByText("Support (unknown)")).toBeTruthy();
  });

  it("lists the hosting server of a toolset with no canonical wrapper under the toolset's name", () => {
    renderRows([
      server({
        id: "wrapper-2",
        name: "Support",
        slug: "wrapper-2",
        toolsetId: "toolset-1",
        toolset: { ...toolset, hostingMcpServerId: "wrapper-2" },
        hasEndpoint: true,
      }),
    ]);
    const link = screen.getByRole("link", {
      name: "Open MCP server Hosted tools",
    });
    expect(link.getAttribute("href")).toBe("/mcp/x/wrapper-2");
    expect(screen.getByText("12 tools")).toBeTruthy();
  });

  it("keeps a wrapper without an endpoint beside the toolset's named host hidden", () => {
    const hosted = { ...toolset, hostingMcpServerId: "toolset-1" };
    renderRows([
      server({ ...HOSTED, toolset: hosted }),
      server({
        id: "wrapper-3",
        name: "Support (gateway)",
        slug: "wrapper-3",
        toolsetId: "toolset-1",
        toolset: hosted,
        hasEndpoint: false,
      }),
    ]);
    expect(
      screen.getAllByRole("link").map((link) => link.getAttribute("href")),
    ).toEqual(["/mcp/x/hosted-wrapper"]);
  });

  it("sends a wrapper with an endpoint waiting on OAuth to its own settings", () => {
    mocks.oauthStatus = "required-unconfigured";
    renderCards([extra("wrapper-2", "Support (staging)", true)]);
    expect(screen.getByText("Set up")).toBeTruthy();
    expect(screen.getByRole("link").getAttribute("href")).toBe(
      "/mcp/x/wrapper-2/settings",
    );
  });
});

describe("MCPServerTableRow", () => {
  it("renders one row per backend kind with its name, kind, address, and contents", () => {
    renderRows([HOSTED, REMOTE, TUNNELED, UNPROXIED]);

    const hosted = screen.getByRole("link", {
      name: "Open MCP server Hosted tools",
    });
    expect(hosted.getAttribute("href")).toBe("/mcp/x/hosted-wrapper");
    expect(screen.getByText("Hosted")).toBeTruthy();
    expect(screen.getByText("mcp.example.com/mcp/hosted")).toBeTruthy();
    expect(screen.getByText("12 tools")).toBeTruthy();

    expect(
      screen
        .getByRole("link", { name: "Open MCP server Docs search" })
        .getAttribute("href"),
    ).toBe("/mcp/x/remote-a1");
    expect(screen.getByText("app.example.com/mcp/remote-a1")).toBeTruthy();
    expect(screen.getByText("Tunneled")).toBeTruthy();
    expect(screen.getByText("tunneled-b2")).toBeTruthy();
    expect(screen.getByText("unproxied-c3")).toBeTruthy();
    expect(
      screen.getAllByRole("button", { name: "Copy MCP URL" }),
    ).toHaveLength(2);
  });

  it("says a proxy-only hosted server lists its tools after sign-in", () => {
    renderRows([
      server({
        ...HOSTED,
        toolset: { ...toolset, toolCount: 0, externalMcpProxyOnly: true },
      }),
    ]);
    expect(screen.getByText("Tools listed once you sign in")).toBeTruthy();
  });

  it("falls back to the server page and claims no count without a toolset summary", () => {
    renderRows([server({ ...HOSTED, toolset: undefined })]);
    const link = screen.getByRole("link", {
      name: "Open MCP server Hosted wrapper",
    });
    expect(link.getAttribute("href")).toBe("/mcp/x/hosted-wrapper");
    expect(screen.queryByText(/tools?$/)).toBeNull();
  });

  it("sends a hosted server still waiting on OAuth to its setup step", () => {
    mocks.oauthStatus = "required-unconfigured";
    renderRows([HOSTED]);
    expect(screen.getByText("OAuth Required")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Open MCP server Hosted tools" })
        .getAttribute("href"),
    ).toBe("/mcp/x/hosted-wrapper/settings#authentication");
  });
});

describe("MCPServerCard", () => {
  it("renders one card per backend kind", () => {
    renderCards([
      server({
        ...HOSTED,
        toolset: { ...toolset, registrySpecifier: "io.example/hosted" },
      }),
      REMOTE,
      TUNNELED,
      UNPROXIED,
    ]);

    expect(screen.getByText("Hosted tools")).toBeTruthy();
    expect(screen.getByText("From io.example/hosted")).toBeTruthy();
    expect(
      screen.getByLabelText("Installed from io.example/hosted"),
    ).toBeTruthy();
    expect(screen.getByText("remote-a1")).toBeTruthy();
    for (const label of ["Hosted", "Remote", "Tunneled", "Unproxied"]) {
      expect(screen.getByText(label, { selector: "span" })).toBeTruthy();
    }
    const hrefs = screen
      .getAllByRole("link")
      .map((link) => link.getAttribute("href"));
    expect(hrefs).toEqual([
      "/mcp/x/hosted-wrapper",
      "/mcp/x/remote-a1",
      "/mcp/x/tunneled-b2",
      "/mcp/x/unproxied-c3",
    ]);
  });

  it("shows the tool count for a hosted server not from the catalog", () => {
    renderCards([HOSTED]);
    expect(screen.getByText("12 tools")).toBeTruthy();
  });

  it("leads with the toolset's description when it has one", () => {
    renderCards([
      server({
        ...HOSTED,
        toolset: {
          ...toolset,
          description: "Answers support questions",
          registrySpecifier: "io.example/hosted",
        },
      }),
    ]);
    expect(screen.getByText("Answers support questions")).toBeTruthy();
    expect(screen.queryByText("From io.example/hosted")).toBeNull();
  });

  it("falls back to the slug and the server page without a toolset summary", () => {
    renderCards([server({ ...HOSTED, toolset: undefined })]);
    expect(screen.getByText("Hosted wrapper")).toBeTruthy();
    expect(screen.getByText("hosted-wrapper")).toBeTruthy();
    expect(screen.queryByText(/tools?$/)).toBeNull();
    expect(screen.getByRole("link").getAttribute("href")).toBe(
      "/mcp/x/hosted-wrapper",
    );
  });
});
