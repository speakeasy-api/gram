import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  server: undefined as unknown,
  error: null as unknown,
}));

vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: () => ({
    data: mocks.server,
    error: mocks.error,
    isLoading: false,
    isError: mocks.error !== null,
  }),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({ data: { mcpEndpoints: [] }, isLoading: false }),
}));
vi.mock("./hosted/useHostedToolset", () => ({
  useHostedToolset: () => ({
    toolset: undefined,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: "disabled" }),
}));
vi.mock("@/hooks/useTabScrollReset", () => ({
  useTabScrollReset: () => ({ current: null }),
}));
vi.mock("../ToolsetRedirect", () => ({
  ToolsetRedirectPage: ({ toolsetSlug }: { toolsetSlug: string }) => (
    <output data-testid="fallback">{toolsetSlug}</output>
  ),
}));
vi.mock("./tabs/OverviewTab", () => ({ OverviewTab: () => null }));
vi.mock("./tabs/InspectTab", () => ({ InspectTab: () => null }));
vi.mock("./tabs/settings/SettingsTab", () => ({ SettingsTab: () => null }));
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
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));

import MCPServerDetails from "./MCPServerDetails";

function Landed() {
  const location = useLocation();
  return (
    <output data-testid="landed">
      {location.pathname + location.search + location.hash}
    </output>
  );
}

function visit(url: string) {
  render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route
          path="/acme/projects/default/mcp/:mcpServerSlug/:tab?"
          element={<MCPServerDetails />}
        />
        <Route
          path="/acme/projects/default/mcp/my-server/settings"
          element={<Landed />}
        />
        <Route
          path="/acme/projects/default/mcp/my-server/inspect"
          element={<Landed />}
        />
        <Route path="/acme/projects/default/mcp" element={<Landed />} />
      </Routes>
    </MemoryRouter>,
  );
}

const REMOTE_SERVER = {
  id: "server-1",
  slug: "my-server",
  remoteMcpServerId: "remote-1",
};

beforeEach(() => {
  mocks.server = REMOTE_SERVER;
  mocks.error = null;
});
afterEach(cleanup);

describe("MCPServerDetails path resolution", () => {
  it("hands a slug no server has to the toolset fallback", () => {
    mocks.server = undefined;
    mocks.error = { statusCode: 404 };
    visit("/acme/projects/default/mcp/my-tools/tools");
    expect(screen.getByTestId("fallback").textContent).toBe("my-tools");
  });

  it("does not fall back when the server exists but cannot be read", () => {
    mocks.server = undefined;
    mocks.error = { statusCode: 403 };
    visit("/acme/projects/default/mcp/locked-server/overview");
    expect(screen.queryByTestId("fallback")).toBeNull();
    expect(screen.getByTestId("landed").textContent).toBe(
      "/acme/projects/default/mcp",
    );
  });

  it.each([
    [
      "/authentication?q=1",
      "/acme/projects/default/mcp/my-server/settings?q=1#authentication",
    ],
    [
      "/performance?q=1",
      "/acme/projects/default/mcp/my-server/settings?q=1#performance",
    ],
    ["/tools?q=1", "/acme/projects/default/mcp/my-server/inspect?q=1"],
    // No tab: the hash picks one, as on the old toolset page.
    [
      "?q=1#performance",
      "/acme/projects/default/mcp/my-server/settings?q=1#performance",
    ],
    ["#inspect", "/acme/projects/default/mcp/my-server/inspect"],
  ])("moves the retired %s tab to %s", (suffix, target) => {
    visit(`/acme/projects/default/mcp/my-server${suffix}`);
    expect(screen.getByTestId("landed").textContent).toBe(target);
  });
});
