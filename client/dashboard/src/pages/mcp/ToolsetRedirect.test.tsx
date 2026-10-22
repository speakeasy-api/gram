import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  toolset: undefined as unknown,
  toolsetError: null as unknown,
  servers: [] as unknown[],
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "default",
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
vi.mock("@/routes", () => {
  const tab = (name: string) => ({
    href: (slug: string) => `/mcp/x/${slug}/${name}`,
  });
  return {
    useRoutes: () => ({
      mcp: {
        Link: ({ children }: { children: React.ReactNode }) => (
          <a href="/mcp">{children}</a>
        ),
        x: {
          overview: tab("overview"),
          tools: tab("tools"),
          resources: tab("resources"),
          prompts: tab("prompts"),
          inspect: tab("inspect"),
          teamAccess: tab("team-access"),
          guardrails: tab("guardrails"),
          sessions: tab("sessions"),
          settings: tab("settings"),
        },
      },
    }),
  };
});

import { ToolsetRedirectPage } from "./ToolsetRedirect";

function Landed() {
  const location = useLocation();
  return (
    <p data-testid="landed">
      {location.pathname + location.search + location.hash}
    </p>
  );
}

function visit(url: string) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route
          path="/acme/projects/default/mcp/:toolsetSlug/*"
          element={<ToolsetRedirectPage />}
        />
        <Route path="/mcp/x/*" element={<Landed />} />
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
    ["", "/mcp/x/my-server/overview"],
    ["/overview", "/mcp/x/my-server/overview"],
    ["/tools", "/mcp/x/my-server/tools"],
    ["/resources", "/mcp/x/my-server/resources"],
    ["/prompts", "/mcp/x/my-server/prompts"],
    ["/authentication", "/mcp/x/my-server/settings#authentication"],
    ["/performance", "/mcp/x/my-server/settings#performance"],
    ["/team-access", "/mcp/x/my-server/team-access"],
    ["/sessions", "/mcp/x/my-server/sessions"],
    ["/settings", "/mcp/x/my-server/settings"],
    ["#authentication", "/mcp/x/my-server/settings#authentication"],
  ])("sends %s to %s", (suffix, target) => {
    visit(`/acme/projects/default/mcp/my-tools${suffix}`);
    expect(screen.getByTestId("landed").textContent).toBe(target);
  });

  it("keeps the query string", () => {
    visit("/acme/projects/default/mcp/my-tools/tools?q=search");
    expect(screen.getByTestId("landed").textContent).toBe(
      "/mcp/x/my-server/tools?q=search",
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
    expect(screen.getByTestId("landed").textContent).toBe("/mcp/x/host/tools");
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
});
