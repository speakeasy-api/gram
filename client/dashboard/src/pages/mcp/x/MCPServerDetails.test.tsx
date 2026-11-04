import userEvent from "@testing-library/user-event";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  server: undefined as unknown,
  error: null as unknown,
  toolSource: {
    status: "ready",
    tools: [
      {
        id: "remote-tool",
        name: "read_records",
        type: "remotemcp",
        annotations: { readOnlyHint: true },
      },
    ],
  } as import("@/pages/access/useServerTools").ToolSource,
  readTools: vi.fn(),
  saveNarrowing:
    vi.fn<(narrowing: { tools: string[]; dispositions: string[] }) => void>(),
  openNarrowing: true,
  hostedToolset: undefined as unknown,
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
    toolset: mocks.hostedToolset,
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
  Page.Section = Object.assign(Page, {
    Title: Page,
    Description: Page,
    Body: Page,
  });
  (Page.Header as unknown as { Breadcrumbs: () => null }).Breadcrumbs = () =>
    null;
  return { Page };
});
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "default" }),
}));

vi.mock("@/pages/access/useServerTools", () => ({
  useServerTools: (entry: unknown) => {
    mocks.readTools(entry);
    return mocks.toolSource;
  },
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));
vi.mock("@/components/insights-context", () => ({
  useHideInsightsDock: vi.fn(),
}));
vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  useResourceAudience: () => ({ data: { entries: [] }, isLoading: false }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
}));
vi.mock("../access/CheckAccess", () => ({ CheckAccess: () => null }));
// Keep the caller, Team access catalogue mapping, and actual narrowing picker
// real; isolate only the audience editor that opens the dialog.
vi.mock("../access/ManageAccess", async () => {
  const { ToolNarrowingDialog } = await import("../access/ToolNarrowingDialog");
  return {
    ManageAccess: ({
      resourceId,
      toolCatalog,
      toolSource,
    }: {
      resourceId: string;
      toolSource?: import("@/pages/access/useServerTools").ToolSource;
      toolCatalog?: import("@/components/tool-selection/ToolSelectionPanel").ToolSelectionTool[];
    }) =>
      mocks.openNarrowing ? (
        <ToolNarrowingDialog
          serverId={resourceId}
          catalog={toolCatalog}
          toolSource={toolSource}
          tools={[]}
          dispositions={[]}
          pending={false}
          onSave={mocks.saveNarrowing}
          onClose={vi.fn<() => void>()}
        />
      ) : null,
  };
});

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
  projectId: "project-1",
};

beforeEach(() => {
  mocks.server = REMOTE_SERVER;
  mocks.error = null;
  mocks.readTools.mockClear();
  mocks.saveNarrowing.mockClear();
  mocks.openNarrowing = true;
  mocks.hostedToolset = undefined;
  mocks.toolSource = {
    status: "ready",
    tools: [
      {
        id: "remote-tool",
        name: "read_records",
        type: "remotemcp",
        annotations: { readOnlyHint: true },
      },
    ],
  };
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

describe("MCPServerDetails Team access tool narrowing", () => {
  it("passes remote tools from the shared source through Team access to the picker", async () => {
    const user = userEvent.setup();
    visit("/acme/projects/default/mcp/my-server/team-access");
    expect(screen.getByRole("dialog")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "By tool" }));
    await user.click(screen.getByRole("checkbox", { name: "read_records" }));
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(mocks.saveNarrowing).toHaveBeenCalledWith({
      tools: ["read_records"],
      dispositions: [],
    });
    expect(mocks.readTools).toHaveBeenCalledWith(
      expect.objectContaining({
        projectId: "project-1",
        server: expect.objectContaining({
          id: "server-1",
          remoteBacked: true,
          dynamicTools: true,
        }),
      }),
    );
  });
});

describe("Team access remote tool source states", () => {
  it("keeps hosted tools and does not open remote discovery", async () => {
    mocks.server = {
      id: "hosted-1",
      slug: "my-server",
      toolsetId: "toolset-1",
      toolset: { hostingMcpServerId: "hosted-1" },
    };
    mocks.hostedToolset = { tools: [{ name: "hosted_read", annotations: {} }] };
    visit("/acme/projects/default/mcp/my-server/team-access");
    await userEvent.click(screen.getByRole("button", { name: "By tool" }));
    expect(screen.getByRole("checkbox", { name: "hosted_read" })).toBeTruthy();
    expect(mocks.readTools).not.toHaveBeenCalled();
  });

  it("keeps tunneled servers dynamic without opening remote discovery", async () => {
    mocks.server = {
      id: "tunnel-1",
      slug: "my-server",
      tunneledMcpServerId: "source-1",
    };
    visit("/acme/projects/default/mcp/my-server/team-access");
    await userEvent.click(screen.getByRole("button", { name: "By tool" }));
    expect(
      screen.getByText("Tools are resolved when the server is called"),
    ).toBeTruthy();
    expect(mocks.readTools).not.toHaveBeenCalled();
  });

  it("shows loading instead of claiming the remote catalogue is empty", () => {
    mocks.openNarrowing = false;
    mocks.toolSource = { status: "loading" };
    visit("/acme/projects/default/mcp/my-server/team-access");
    expect(screen.getByText("Loading server tools…")).toBeTruthy();
  });

  it("offers retry when remote discovery fails", async () => {
    const retry = vi.fn<() => void>();
    mocks.openNarrowing = false;
    mocks.toolSource = { status: "error", retry };
    visit("/acme/projects/default/mcp/my-server/team-access");
    await userEvent.click(
      screen.getByRole("button", { name: "Retry loading tools" }),
    );
    expect(retry).toHaveBeenCalledOnce();
  });

  it("offers the shared first-party connect action when needed", async () => {
    const connect = vi.fn<() => void>();
    mocks.openNarrowing = false;
    mocks.toolSource = { status: "needs-connect", connect };
    visit("/acme/projects/default/mcp/my-server/team-access");
    await userEvent.click(
      screen.getByRole("button", { name: "Connect to load tools" }),
    );
    expect(connect).toHaveBeenCalledOnce();
  });

  it("explains write-gated discovery without offering connect", () => {
    mocks.openNarrowing = false;
    mocks.toolSource = { status: "needs-write" };
    visit("/acme/projects/default/mcp/my-server/team-access");
    expect(screen.getByText(/Someone with write access/)).toBeTruthy();
    expect(
      screen.queryByRole("button", { name: "Connect to load tools" }),
    ).toBeNull();
  });
});

describe("Team access unavailable tool picker states", () => {
  it.each([
    [{ status: "loading" }, "Loading tools…"],
    [
      { status: "error", retry: vi.fn<() => void>() },
      "Couldn't load this server's tools.",
    ],
    [
      { status: "needs-write" },
      "Someone with write access to this server must load its tools before they can be selected here.",
    ],
    [
      { status: "needs-connect", connect: vi.fn<() => void>() },
      "Connect to this server to load its tools.",
    ],
    [
      { status: "needs-connect", connect: undefined },
      "Connect isn't available for this server. Review its authentication settings.",
    ],
  ] as const)(
    "preserves %j inside the narrowing dialog",
    async (source, message) => {
      mocks.toolSource = source;
      visit("/acme/projects/default/mcp/my-server/team-access");
      const dialog = within(screen.getByRole("dialog"));
      await userEvent.click(dialog.getByRole("button", { name: "By tool" }));
      expect(dialog.getByText(message)).toBeTruthy();
      expect(
        dialog.queryByText("Tools are resolved when the server is called"),
      ).toBeNull();
      if (source.status === "error") {
        await userEvent.click(dialog.getByRole("button", { name: "Retry" }));
        expect(source.retry).toHaveBeenCalled();
      }
    },
  );

  it("does not instruct connecting when no connect action exists", () => {
    mocks.openNarrowing = false;
    mocks.toolSource = { status: "needs-connect", connect: undefined };
    visit("/acme/projects/default/mcp/my-server/team-access");
    expect(
      screen.getByText(
        "Connect isn't available for this server. Review its authentication settings.",
      ),
    ).toBeTruthy();
    expect(
      screen.queryByText("Connect to this server to load its tools."),
    ).toBeNull();
    expect(
      screen.queryByRole("button", { name: "Connect to load tools" }),
    ).toBeNull();
  });
});
