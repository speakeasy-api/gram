import { cleanup, render, renderHook, screen } from "@testing-library/react";
import {
  createRoutesFromElements,
  matchRoutes,
  MemoryRouter,
  Route,
  Routes,
  useLocation,
  useParams,
} from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: ({ slug }: { slug?: string }) => ({
    data: slug === "gateway" ? { id: "server-id" } : undefined,
    isLoading: false,
  }),
}));
vi.mock("./pages/mcp/x/MCPServerDetails", () => ({
  default: function ServerPageProbe() {
    const { mcpServerSlug } = useParams();
    const location = useLocation();
    return (
      <output data-testid="server-page">
        {`${mcpServerSlug} ${location.pathname}${location.search}${location.hash}`}
      </output>
    );
  },
}));

import { mcpServerRouteParam, RESERVED_MCP_SEGMENTS } from "./lib/sources";
import { legacyMcpRedirects, routesWithSubroutes } from "./route-elements";
import { useRoutes, type AppRoute } from "./routes";

const BASE = "/acme/projects/default";

// The routes App.tsx mounts inside the project layout.
function layoutRoutes(routes: ReturnType<typeof useRoutes>): AppRoute[] {
  return Object.values(routes).filter(
    (route) => !route.unauthenticated && !route.outsideMainLayout,
  );
}

function ProjectTree() {
  const routes = useRoutes();
  return (
    <Routes>
      <Route path=":orgSlug/projects/:projectSlug">
        {routesWithSubroutes(layoutRoutes(routes))}
        {legacyMcpRedirects}
        <Route path="*" element={<p>not found</p>} />
      </Route>
    </Routes>
  );
}

function visit(path: string): string {
  render(
    <MemoryRouter initialEntries={[path]}>
      <ProjectTree />
    </MemoryRouter>,
  );
  return screen.getByTestId("server-page").textContent ?? "";
}

function projectRouteObjects() {
  const { result } = renderHook(() => useRoutes(), {
    wrapper: ({ children }) => <MemoryRouter>{children}</MemoryRouter>,
  });
  return createRoutesFromElements(
    <Route path="/:orgSlug/projects/:projectSlug">
      {routesWithSubroutes(layoutRoutes(result.current))}
      {legacyMcpRedirects}
    </Route>,
  );
}

afterEach(cleanup);

describe("MCP server routes", () => {
  it.each([
    ["/mcp/my-server-ab12", "my-server-ab12 /mcp/my-server-ab12"],
    [
      "/mcp/my-server-ab12/settings?q=1#authentication",
      "my-server-ab12 /mcp/my-server-ab12/settings?q=1#authentication",
    ],
    // Legacy toolset URLs reach the server page, which resolves them.
    ["/mcp/my-tools/performance", "my-tools /mcp/my-tools/performance"],
    ["/mcp/my-tools/authentication", "my-tools /mcp/my-tools/authentication"],
  ])("serves %s from the server page", (path, landed) => {
    expect(visit(`${BASE}${path}`)).toBe(landed.replace(" /", ` ${BASE}/`));
  });

  it.each([
    ["/mcp/x/my-server-ab12", "my-server-ab12 /mcp/my-server-ab12"],
    [
      "/mcp/x/my-server-ab12/inspect?q=1#tools",
      "my-server-ab12 /mcp/my-server-ab12/inspect?q=1#tools",
    ],
    [
      "/mcp/x/my-server-ab12/authentication",
      "my-server-ab12 /mcp/my-server-ab12/authentication",
    ],
    ["/mcp/x/my%20server/settings", "my server /mcp/my%20server/settings"],
    // A retired link whose server slug is a tab name, followed by a tab.
    ["/mcp/x/overview/settings", "overview /mcp/overview/settings"],
    // A slug a static segment would claim moves to the server's id.
    [
      "/mcp/x/gateway/settings?q=1#authentication",
      "server-id /mcp/server-id/settings?q=1#authentication",
    ],
  ])("redirects the retired %s", (path, landed) => {
    expect(visit(`${BASE}${path}`)).toBe(landed.replace(" /", ` ${BASE}/`));
  });

  it.each([
    ["/mcp/x", "x /mcp/x"],
    // A tab of a server or toolset slugged x outranks the retired namespace;
    // the server page falls back to it when no such server or toolset exists.
    ["/mcp/x/overview", "x /mcp/x/overview"],
    ["/mcp/x/tools", "x /mcp/x/tools"],
  ])("keeps %s for a server or toolset slugged x", (path, landed) => {
    expect(visit(`${BASE}${path}`)).toBe(landed.replace(" /", ` ${BASE}/`));
  });

  it.each([
    "/toolsets/my-tools",
    "/toolsets/my-tools/settings",
    "/toolsets/my-tools/authentication",
    "/toolsets/my-tools/performance",
  ])("sends the explicit toolset link %s to the toolset resolver", (path) => {
    const matches = matchRoutes(projectRouteObjects(), `${BASE}${path}`) ?? [];
    expect(matches.map((match) => match.route.path)).toContain(
      "toolsets/:toolsetSlug",
    );
  });

  it.each([
    "/mcp/add",
    "/mcp/add/remote",
    "/mcp/catalog",
    "/mcp/catalog/some-server",
    "/mcp/deployments",
    "/mcp/sources",
    "/mcp/sources/source-1",
    "/mcp/organization-gateway",
    "/mcp/gateway/gateway-1",
    "/mcp/gateway/gateway-1/overview",
  ])("leaves the static %s to its own page", (path) => {
    const matches = matchRoutes(projectRouteObjects(), `${BASE}${path}`) ?? [];
    expect(matches.length).toBeGreaterThan(0);
    expect(matches.map((match) => match.route.path)).not.toContain(
      ":mcpServerSlug",
    );
  });

  it("links servers slugged like a static segment by id", () => {
    const { result } = renderHook(() => useRoutes(), {
      wrapper: ({ children }) => <MemoryRouter>{children}</MemoryRouter>,
    });
    const staticSegments = Object.values(result.current.mcp)
      .filter(
        (value): value is AppRoute =>
          !!value && typeof value === "object" && "url" in value,
      )
      .map((route) => route.url.split("/")[0] ?? "")
      .filter((segment) => segment !== "" && !segment.startsWith(":"));
    expect(staticSegments.length).toBeGreaterThan(0);
    for (const segment of [...staticSegments, "x"]) {
      expect(RESERVED_MCP_SEGMENTS.has(segment)).toBe(true);
      expect(mcpServerRouteParam({ id: "server-id", slug: segment })).toBe(
        "server-id",
      );
    }
    expect(mcpServerRouteParam({ id: "server-id", slug: "my-server" })).toBe(
      "my-server",
    );
  });
});
