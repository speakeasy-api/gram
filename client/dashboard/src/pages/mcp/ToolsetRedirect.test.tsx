import { cleanup, render, screen } from "@testing-library/react";
import {
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useParams,
} from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  toolset: undefined as unknown,
  toolsetError: null as unknown,
  servers: [] as unknown[],
}));

// Redirect targets resolve under their own org slug so the landing route can
// never be confused with the toolset and server routes the redirect left.
vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
  useSlugs: () => ({ orgSlug: "landed", projectSlug: "default" }),
}));
vi.mock("@/hooks/toolTypes", () => ({
  useToolset: () => ({
    data: mocks.toolset,
    error: mocks.toolsetError,
    isLoading: false,
  }),
}));
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: () => ({
    data: { mcpServers: mocks.servers },
    error: null,
    isLoading: false,
  }),
}));
vi.mock("@/components/page-layout", () => {
  const Page = ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  );
  Page.Header = Page;
  Page.Body = Page;
  (Page.Header as unknown as { Breadcrumbs: () => null }).Breadcrumbs = () =>
    null;
  return { Page };
});

import {
  ToolsetRedirectPage,
  ToolsetSlugRedirectPage,
} from "./ToolsetRedirect";
import { tabSegmentFromPath } from "./x/MCPServerDetailsRouting";

function Landed() {
  const location = useLocation();
  return (
    <p data-testid="landed">
      {location.pathname + location.search + location.hash}
    </p>
  );
}

function Fallback() {
  const { slug = "" } = useParams<{ slug: string }>();
  const location = useLocation();
  return (
    <ToolsetRedirectPage
      toolsetSlug={slug}
      tabSegment={tabSegmentFromPath(location.pathname, slug)}
      serverLookupMissed
    />
  );
}

function visit(url: string) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route
          path="/acme/projects/default/toolsets/:toolsetSlug/*"
          element={<ToolsetSlugRedirectPage />}
        />
        <Route
          path="/acme/projects/default/mcp/:slug/*"
          element={<Fallback />}
        />
        <Route
          path="/acme/projects/default/mcp/settings"
          element={<Landed />}
        />
        <Route path="/landed/*" element={<Landed />} />
      </Routes>
    </MemoryRouter>,
  );
}

const TOOLSET = { id: "toolset-1", slug: "my-tools", name: "My tools" };

beforeEach(() => {
  mocks.toolset = TOOLSET;
  mocks.toolsetError = null;
  mocks.servers = [
    {
      id: "toolset-1",
      slug: "my-server",
      toolsetId: "toolset-1",
      toolset: { hostingMcpServerId: "toolset-1" },
    },
  ];
});
afterEach(cleanup);

describe("ToolsetRedirectPage", () => {
  it.each([
    ["", "/landed/projects/default/mcp/my-server/overview"],
    ["/overview", "/landed/projects/default/mcp/my-server/overview"],
    ["/tools", "/landed/projects/default/mcp/my-server/tools"],
    ["/resources", "/landed/projects/default/mcp/my-server/resources"],
    ["/prompts", "/landed/projects/default/mcp/my-server/prompts"],
    [
      "/authentication",
      "/landed/projects/default/mcp/my-server/settings#authentication",
    ],
    [
      "/performance",
      "/landed/projects/default/mcp/my-server/settings#performance",
    ],
    ["/team-access", "/landed/projects/default/mcp/my-server/team-access"],
    ["/sessions", "/landed/projects/default/mcp/my-server/sessions"],
    ["/settings", "/landed/projects/default/mcp/my-server/settings"],
    [
      "#authentication",
      "/landed/projects/default/mcp/my-server/settings#authentication",
    ],
  ])("sends %s to %s", (suffix, target) => {
    visit(`/acme/projects/default/mcp/my-tools${suffix}`);
    expect(screen.getByTestId("landed").textContent).toBe(target);
  });

  it("keeps the query string", () => {
    visit("/acme/projects/default/mcp/my-tools/tools?q=search");
    expect(screen.getByTestId("landed").textContent).toBe(
      "/landed/projects/default/mcp/my-server/tools?q=search",
    );
  });

  it("sends a toolset with no canonical wrapper to its hosting server", () => {
    mocks.servers = [
      {
        id: "member-1",
        slug: "member",
        toolsetId: "toolset-1",
        toolset: { hostingMcpServerId: "host-1" },
      },
      {
        id: "host-1",
        slug: "host",
        toolsetId: "toolset-1",
        toolset: { hostingMcpServerId: "host-1" },
      },
    ];
    visit("/acme/projects/default/mcp/my-tools/tools");
    expect(screen.getByTestId("landed").textContent).toBe(
      "/landed/projects/default/mcp/host/tools",
    );
  });

  it("says so when no server hosts the toolset", () => {
    mocks.servers = [
      {
        id: "member-1",
        slug: "member",
        toolsetId: "toolset-1",
        toolset: {},
      },
    ];
    visit("/acme/projects/default/mcp/my-tools");
    expect(screen.getByText("My tools has no MCP server")).toBeDefined();
  });

  it("drops the retired x segment when no toolset is slugged x", () => {
    mocks.toolset = undefined;
    mocks.toolsetError = { statusCode: 404 };
    visit("/acme/projects/default/mcp/x/settings?q=1#authentication");
    expect(screen.getByTestId("landed").textContent).toBe(
      "/acme/projects/default/mcp/settings?q=1#authentication",
    );
  });

  it("resolves a toolset slugged x before the retired x segment", () => {
    mocks.toolset = { ...TOOLSET, slug: "x" };
    visit("/acme/projects/default/mcp/x/tools");
    expect(screen.getByTestId("landed").textContent).toBe(
      "/landed/projects/default/mcp/my-server/tools",
    );
  });

  it("does not redirect to the slug the server lookup already missed", () => {
    mocks.servers = [
      {
        id: "toolset-1",
        slug: "my-tools",
        toolsetId: "toolset-1",
        toolset: { hostingMcpServerId: "toolset-1" },
      },
    ];
    visit("/acme/projects/default/mcp/my-tools/tools");
    expect(screen.getByText("MCP server not found")).toBeDefined();
  });

  it("says not found for an unknown slug", () => {
    mocks.toolset = undefined;
    mocks.toolsetError = { statusCode: 404 };
    visit("/acme/projects/default/mcp/nothing/tools");
    expect(screen.getByText("MCP server not found")).toBeDefined();
  });

  it("treats an unreadable toolset as not found", () => {
    mocks.toolset = undefined;
    mocks.toolsetError = { statusCode: 403 };
    visit("/acme/projects/default/mcp/my-tools");
    expect(screen.getByText("MCP server not found")).toBeDefined();
  });

  it("drops the retired x segment when the toolset slugged x has no server", () => {
    mocks.toolset = { ...TOOLSET, slug: "x" };
    mocks.servers = [];
    visit("/acme/projects/default/mcp/x/settings");
    expect(screen.getByTestId("landed").textContent).toBe(
      "/acme/projects/default/mcp/settings",
    );
  });

  it.each([
    ["", "/landed/projects/default/mcp/my-server/overview"],
    ["/tools?q=1", "/landed/projects/default/mcp/my-server/tools?q=1"],
    [
      "#performance",
      "/landed/projects/default/mcp/my-server/settings#performance",
    ],
  ])("resolves the explicit toolset route %s", (suffix, target) => {
    visit(`/acme/projects/default/toolsets/my-tools${suffix}`);
    expect(screen.getByTestId("landed").textContent).toBe(target);
  });

  it("follows the explicit toolset route even when the host shares its slug", () => {
    mocks.servers = [
      {
        id: "toolset-1",
        slug: "my-tools",
        toolsetId: "toolset-1",
        toolset: { hostingMcpServerId: "toolset-1" },
      },
    ];
    visit("/acme/projects/default/toolsets/my-tools/tools");
    expect(screen.getByTestId("landed").textContent).toBe(
      "/landed/projects/default/mcp/my-tools/tools",
    );
  });
});
