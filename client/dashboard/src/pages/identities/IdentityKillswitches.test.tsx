import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TooltipProvider } from "@/components/ui/Tooltip";
import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import { IdentityKillswitches } from "./IdentityKillswitches";

const state = vi.hoisted(() => ({
  canAccess: true,
  memberId: "member-1" as string | undefined,
  pages: [[]] as unknown[][],
  hasNextPage: false,
  fetchNextPage: vi.fn(),
  servers: [{ id: "server-1", name: "Linear" }],
  capabilities: [{ key: "mcp_tool_calls", label: "MCP tool calls" }],
  editorProps: undefined as Record<string, unknown> | undefined,
  recordProps: undefined as Record<string, unknown> | undefined,
}));

vi.mock("@/hooks/useKillswitchAccess", () => ({
  useKillswitchAccess: () => ({
    canAccess: state.canAccess,
    isLoading: false,
    reason: state.canAccess ? "allowed" : "scope",
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "session-1" }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    killswitch: {
      detail: { href: (id: string) => `/acme/killswitch/${id}`, goTo: vi.fn() },
    },
    mcpSessions: { href: () => "/acme/mcp-sessions" },
  }),
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/killswitches.js", () => ({
  useKillswitchesInfinite: () => ({
    data: { pages: state.pages.map((items) => ({ result: { items } })) },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    hasNextPage: state.hasNextPage,
    isFetchingNextPage: false,
    fetchNextPage: state.fetchNextPage,
  }),
  invalidateAllKillswitches: vi.fn(),
}));
vi.mock("@gram/client/react-query/killswitchMCPServers.js", () => ({
  useKillswitchMCPServers: () => ({
    data: { servers: state.servers },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/killswitchCapabilities.js", () => ({
  useKillswitchCapabilities: () => ({
    data: { capabilities: state.capabilities, comingSoon: [] },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/createKillswitch.js", () => ({
  useCreateKillswitchMutation: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@gram/client/react-query/previewKillswitchOverlaps.js", () => ({
  usePreviewKillswitchOverlapsMutation: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@/components/FeatureRequestModal", () => ({
  FeatureRequestModal: () => null,
}));
vi.mock("@/components/killswitch/KillswitchRecord", () => ({
  KillswitchRecord: (props: Record<string, unknown>) => {
    state.recordProps = props;
    return <div data-testid="record" />;
  },
}));
vi.mock("@/components/killswitch/KillswitchEditorSheet", () => ({
  KillswitchEditorSheet: (props: Record<string, unknown>) => {
    state.editorProps = props;
    return <div data-testid="editor" />;
  },
}));
vi.mock("./useIdentityQueries", () => ({
  retryFailed: () => vi.fn(),
  useIdentityMember: () => ({
    member: state.memberId
      ? { id: state.memberId, name: "Ada", email: "ada@example.com" }
      : undefined,
    query: {
      data: {
        members: state.memberId
          ? [{ id: state.memberId, name: "Ada", email: "ada@example.com" }]
          : [],
      },
      isLoading: false,
      isError: false,
      refetch: vi.fn(),
    },
  }),
}));

const identity = {
  displayName: "Ada",
  emails: ["ada@example.com"],
  externalUserIds: [],
  kind: "user",
  userIds: [],
} as unknown as IdentityModel;

function summary(overrides: Record<string, unknown>) {
  return {
    id: "ks-1",
    userId: "member-1",
    version: 1,
    capabilityKey: "mcp_tool_calls",
    capabilityLabel: "MCP tool calls",
    scope: { type: "all_servers" },
    schedule: { start: "now", end: "until_lifted" },
    status: "active",
    ...overrides,
  };
}

function renderPanel(search = "") {
  return render(
    <TooltipProvider>
      <MemoryRouter initialEntries={[`/identities/user%3Aada/access${search}`]}>
        <IdentityKillswitches identity={identity} />
      </MemoryRouter>
    </TooltipProvider>,
  );
}

afterEach(cleanup);

beforeEach(() => {
  state.canAccess = true;
  state.memberId = "member-1";
  state.pages = [[]];
  state.hasNextPage = false;
  state.fetchNextPage.mockReset();
  state.servers = [{ id: "server-1", name: "Linear" }];
  state.capabilities = [{ key: "mcp_tool_calls", label: "MCP tool calls" }];
  state.editorProps = undefined;
  state.recordProps = undefined;
});

describe("IdentityKillswitches", () => {
  it("shows no panel at all to a reader who cannot use killswitches", () => {
    state.canAccess = false;
    const view = renderPanel();
    expect(view.container.textContent).toBe("");
  });

  it("lists this person's killswitches against their own records", () => {
    state.pages = [
      [
        summary({ id: "ks-1", status: "active" }),
        summary({ id: "ks-2", status: "scheduled" }),
      ],
    ];
    renderPanel();

    expect(
      screen
        .getAllByRole("link")
        .map((link) => link.getAttribute("href"))
        .filter((href) => href?.includes("killswitch=")),
    ).toEqual([
      "/identities/user%3Aada/access?killswitch=ks-1",
      "/identities/user%3Aada/access?killswitch=ks-2",
    ]);
    expect(screen.getByText("1 in force · 1 scheduled")).toBeDefined();
  });

  it("opens a row's record on this same page, addressably", async () => {
    state.pages = [[summary({ id: "ks-1" })]];
    renderPanel();

    expect(
      screen
        .getAllByRole("link")
        .map((link) => link.getAttribute("href"))
        .filter((href) => href?.includes("killswitch=")),
    ).toEqual(["/identities/user%3Aada/access?killswitch=ks-1"]);
    expect(screen.queryByTestId("record")).toBeNull();
  });

  it("shows the selected record against the person whose page it is", async () => {
    state.pages = [[summary({ id: "ks-1" })]];
    renderPanel("?killswitch=ks-1");

    await screen.findByTestId("record");
    expect(state.recordProps?.killswitchId).toBe("ks-1");
    expect(state.recordProps?.subjectUserId).toBe("member-1");
  });

  it("reaches past the first page instead of capping the list", () => {
    state.pages = [[summary({ id: "ks-1" })]];
    state.hasNextPage = true;
    renderPanel();

    fireEvent.click(screen.getByRole("button", { name: "Load more" }));
    expect(state.fetchNextPage).toHaveBeenCalled();

    // The next page's rows are reachable once it lands, not just countable.
    state.pages = [[summary({ id: "ks-1" })], [summary({ id: "ks-26" })]];
    state.hasNextPage = false;
    cleanup();
    renderPanel();
    expect(
      screen
        .getAllByRole("link")
        .map((link) => link.getAttribute("href"))
        .filter((href) => href?.includes("killswitch=")),
    ).toEqual([
      "/identities/user%3Aada/access?killswitch=ks-1",
      "/identities/user%3Aada/access?killswitch=ks-26",
    ]);
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("does not report a total it has not finished counting", () => {
    state.pages = [
      [
        summary({ id: "ks-1", status: "active" }),
        summary({ id: "ks-2", status: "lifted" }),
      ],
    ];
    state.hasNextPage = true;
    renderPanel();

    expect(
      screen.getByText("1 in force · 0 scheduled of the 2 loaded so far"),
    ).toBeDefined();
  });

  it("says nothing can be turned off for an identity with no member row", () => {
    state.memberId = undefined;
    renderPanel();

    expect(
      screen.getByText(/No org member row resolves to this identity/),
    ).toBeDefined();
    expect(screen.queryByRole("button", { name: "New killswitch" })).toBeNull();
  });

  it("opens the editor on this person, carrying what the sender was looking at", async () => {
    renderPanel(
      "?create=1&createCapability=mcp_tool_calls&originServer=server-1",
    );

    await screen.findByTestId("editor");
    expect(state.editorProps?.createContext).toEqual({
      userId: "member-1",
      capabilityKey: "mcp_tool_calls",
      originatingMcpServerId: "server-1",
    });
  });

  it("drops a preselection the catalog no longer has", async () => {
    state.servers = [];
    state.capabilities = [];
    renderPanel(
      "?create=1&createCapability=mcp_tool_calls&originServer=server-1",
    );

    await screen.findByTestId("editor");
    expect(state.editorProps?.createContext).toEqual({
      userId: "member-1",
      capabilityKey: undefined,
      originatingMcpServerId: undefined,
    });
  });
});
