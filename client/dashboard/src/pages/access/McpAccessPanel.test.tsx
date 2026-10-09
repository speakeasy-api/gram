import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { Selector } from "@gram/client/models/components/selector.js";
import { useState } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { McpAccessPanel } from "./McpAccessPanel";
import type { ServerGroup } from "./serverMerge";
import type { RoleGrant } from "./types";

const inventory = vi.hoisted(() => ({
  groups: [] as ServerGroup[],
}));
const live = vi.hoisted(() => ({
  connect: vi.fn(),
  needsAuth: true,
  tools: undefined as
    | Record<string, { annotations?: { readOnlyHint?: boolean } }>
    | undefined,
  lastOptions: undefined as { enabled?: boolean } | undefined,
  canWrite: true,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => live.canWrite }),
}));

vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: () => ({
    data: {
      id: "remote",
      userSessionIssuerId: "issuer",
      visibility: "private",
    },
    isLoading: false,
    isError: false,
    refetch: () => {},
  }),
}));
vi.mock("@gram/client/react-query/mcpEndpoints.js", () => ({
  useMcpEndpoints: () => ({
    data: { mcpEndpoints: [{ slug: "remote-slug" }] },
    isLoading: false,
    isError: false,
    refetch: () => {},
  }),
}));
vi.mock("@/pages/mcp/x/tabs/useRemoteMcpToolConnection", () => ({
  useRemoteMcpToolConnection: (options: { enabled?: boolean }) => {
    live.lastOptions = options;
    return {
      tools: options.enabled ? live.tools : undefined,
      metadataByTool: {},
      loading: !options.enabled,
      needsAuth: !!options.enabled && live.needsAuth,
      isError: false,
      isIssuerGated: true,
      refetch: () => {},
      connect: live.connect,
      sync: () => {},
      isSyncing: false,
    };
  },
}));

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    projects: [
      { id: "p-default", slug: "default", name: "Default" },
      { id: "p-data", slug: "data", name: "Data Platform" },
    ],
  }),
}));
vi.mock("@/routes", () => ({
  useRoutes: () => ({ mcp: { x: { inspect: { href: () => "/inspect" } } } }),
}));
vi.mock("@/hooks/useToolMetadata", () => ({
  useToolMetadata: () => ({
    metadataByTool: {},
    isLoading: false,
    isError: false,
    refetch: () => {},
  }),
}));
vi.mock("./useOrgMcpServers", () => ({
  useOrgMcpServers: () => ({
    groups: inventory.groups,
    settled: true,
    isError: false,
    refetch: () => {},
  }),
}));

const server = (id: string, name: string) => ({
  id,
  name,
  slug: id,
  tools: [
    {
      id: `${id}-search`,
      name: "search",
      type: "http",
      annotations: { readOnlyHint: true },
    },
  ],
  dynamicTools: false,
  remoteBacked: false,
});

const connect = (allow: Selector[] | null, deny?: Selector[]): RoleGrant => ({
  scope: "mcp:connect" as Scope,
  rules: [
    { id: "a", effect: "allow", selectors: allow },
    ...(deny ? [{ id: "d", effect: "deny" as const, selectors: deny }] : []),
  ],
});

function renderPanel(initial: Record<string, RoleGrant> = {}) {
  const onChange = vi.fn();
  const onShowPlatformAccess = vi.fn();
  // Feeds each emitted grant back in, as the role editor does.
  function Harness() {
    const [grants, setGrants] = useState(initial);
    return (
      <McpAccessPanel
        grants={grants}
        onChangeConnectGrant={(grant) => {
          onChange(grant);
          setGrants((prev) => {
            const next = { ...prev };
            if (grant) next["mcp:connect"] = grant;
            else delete next["mcp:connect"];
            return next;
          });
        }}
        onShowPlatformAccess={() => {
          onShowPlatformAccess();
        }}
      />
    );
  }
  render(
    <MemoryRouter>
      <Harness />
    </MemoryRouter>,
  );
  return { onChange, onShowPlatformAccess };
}

const allowSelectors = (grant: RoleGrant | undefined) =>
  grant?.rules.find((r) => r.effect === "allow")?.selectors;
const denySelectors = (grant: RoleGrant | undefined) =>
  grant?.rules.find((r) => r.effect === "deny")?.selectors;

beforeEach(() => {
  live.connect.mockReset();
  live.needsAuth = true;
  live.tools = undefined;
  live.canWrite = true;
  inventory.groups = [
    {
      projectId: "p-default",
      projectName: "Default",
      servers: [
        server("linear", "Linear"),
        server("slack", "Slack"),
        {
          id: "remote",
          name: "Remote Docs",
          slug: "remote-docs",
          tools: [],
          dynamicTools: true,
          remoteBacked: true,
        },
      ],
    },
    {
      projectId: "p-data",
      projectName: "Data Platform",
      servers: [server("snowflake", "Snowflake")],
    },
  ];
});
afterEach(cleanup);

describe("McpAccessPanel", () => {
  it("opens the default project and collapses the rest with a count", () => {
    renderPanel({
      "mcp:connect": connect([
        { resourceKind: "mcp", resourceId: "snowflake" },
      ]),
    });
    expect(screen.getByLabelText("Linear")).toBeTruthy();
    expect(screen.queryByLabelText("Snowflake")).toBeNull();
    expect(screen.getByLabelText("1 of 1 servers selected")).toBeTruthy();
  });

  it("grants a ticked server with every tool", () => {
    const { onChange } = renderPanel();
    fireEvent.click(screen.getByLabelText("Linear"));
    expect(allowSelectors(onChange.mock.calls.at(-1)?.[0])).toEqual([
      { resourceKind: "mcp", resourceId: "linear" },
    ]);
  });

  it("locks servers the role administers", () => {
    const { onShowPlatformAccess } = renderPanel({
      "mcp:read": {
        scope: "mcp:read" as Scope,
        rules: [
          {
            id: "r",
            effect: "allow",
            selectors: [{ resourceKind: "mcp", resourceId: "slack" }],
          },
        ],
      },
    });
    const slack = screen.getByLabelText("Slack") as HTMLButtonElement;
    expect(slack.disabled).toBe(true);
    expect(slack.getAttribute("data-state")).toBe("checked");
    fireEvent.click(
      screen.getByRole("button", {
        name: "Slack is always on: administrative access through mcp:read",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Manage in Platform access" }),
    );
    expect(onShowPlatformAccess).toHaveBeenCalled();
  });

  it("lists forbidden servers apart and unblocks them", () => {
    const { onChange } = renderPanel({
      "mcp:connect": connect(
        [{ resourceKind: "mcp", resourceId: "linear" }],
        [{ resourceKind: "mcp", resourceId: "slack" }],
      ),
    });
    expect(screen.queryByLabelText("Slack")).toBeNull();
    fireEvent.click(
      screen.getByRole("button", { name: "Remove Slack from Forbidden" }),
    );
    const grant = onChange.mock.calls.at(-1)?.[0] as RoleGrant;
    expect(denySelectors(grant)).toBeUndefined();
    expect(allowSelectors(grant)).toEqual([
      { resourceKind: "mcp", resourceId: "linear" },
    ]);
  });

  it("hides the picker and grants every server for All servers", () => {
    const { onChange } = renderPanel();
    fireEvent.click(screen.getByRole("radio", { name: "All servers" }));
    expect(allowSelectors(onChange.mock.calls.at(-1)?.[0])).toBeNull();
    expect(screen.queryByPlaceholderText("Search servers")).toBeNull();
  });

  it("filters servers by fuzzy search", () => {
    renderPanel();
    fireEvent.change(screen.getByPlaceholderText("Search servers"), {
      target: { value: "lnr" },
    });
    expect(screen.getByLabelText("Linear")).toBeTruthy();
    expect(screen.queryByLabelText("Slack")).toBeNull();
  });

  it("says when the role holds rules it cannot show", () => {
    renderPanel({
      "mcp:connect": connect([
        { resourceKind: "mcp", resourceId: "*", projectId: "p-data" },
      ]),
    });
    expect(screen.getByText(/rules this view can.t show/)).toBeTruthy();
  });

  it("shows a tool limit as a badge", () => {
    renderPanel({
      "mcp:connect": connect([
        { resourceKind: "mcp", resourceId: "linear", disposition: "read_only" },
      ]),
    });
    expect(
      screen.getByRole("button", { name: "Read-Only Tools" }),
    ).toBeTruthy();
  });

  it("edits a server's tools in the sheet, carrying access across", () => {
    const { onChange } = renderPanel({
      "mcp:connect": connect([
        { resourceKind: "mcp", resourceId: "linear", disposition: "read_only" },
      ]),
    });
    fireEvent.click(screen.getByRole("button", { name: "Read-Only Tools" }));
    expect(screen.getByRole("dialog", { name: /Linear/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "By tool" }));
    expect(allowSelectors(onChange.mock.calls.at(-1)?.[0])).toEqual([
      { resourceKind: "mcp", resourceId: "linear", tool: "search" },
    ]);
    expect(screen.getByText("1 of 1 tools selected")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Clear" }));
    // Nothing is granted, but the server stays chosen and says so.
    expect(onChange.mock.calls.at(-1)?.[0]).toBeUndefined();
    expect(screen.getByText("0 of 1 tools selected")).toBeTruthy();
  });

  it("signs in from the sheet when a remote server has no tools stored", () => {
    renderPanel({
      "mcp:connect": connect([
        { resourceKind: "mcp", resourceId: "remote", tool: "search" },
      ]),
    });
    fireEvent.click(screen.getByRole("button", { name: "1 Tool" }));
    expect(live.lastOptions?.enabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(live.connect).toHaveBeenCalled();
  });

  it("lists a remote server's tools from a live session", () => {
    live.needsAuth = false;
    live.tools = {
      search: { annotations: { readOnlyHint: true } },
      purge: {},
    };
    renderPanel({
      "mcp:connect": connect([
        { resourceKind: "mcp", resourceId: "remote", tool: "search" },
      ]),
    });
    fireEvent.click(screen.getByRole("button", { name: "1 Tool" }));
    expect(screen.getByText("1 of 2 tools selected")).toBeTruthy();
  });

  it("asks for mcp:write instead of connecting when the editor cannot record tools", () => {
    live.canWrite = false;
    renderPanel({
      "mcp:connect": connect([
        { resourceKind: "mcp", resourceId: "remote", tool: "search" },
      ]),
    });
    fireEvent.click(screen.getByRole("button", { name: "1 Tool" }));
    expect(
      screen.getByText("Setting tool-level permissions requires mcp:write"),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Connect" })).toBeNull();
    expect(live.lastOptions?.enabled).toBe(false);
  });

  it("ticks a server from anywhere on its row, once per click", () => {
    const { onChange } = renderPanel();
    fireEvent.click(screen.getByText("slack"));
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(allowSelectors(onChange.mock.calls.at(-1)?.[0])).toEqual([
      { resourceKind: "mcp", resourceId: "slack" },
    ]);
    // The checkbox inside the row toggles once, not once for it and once
    // for the row.
    fireEvent.click(screen.getByLabelText("Slack"));
    expect(onChange).toHaveBeenCalledTimes(2);
    expect(onChange.mock.calls.at(-1)?.[0]).toBeUndefined();
  });

  it("opens an ungranted server's sheet from its menu with nothing chosen", () => {
    const { onChange } = renderPanel();
    const trigger = screen.getByRole("button", {
      name: "More options for Slack",
    });
    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
    fireEvent.click(screen.getByRole("menuitem", { name: /Edit by tool/ }));
    expect(screen.getByRole("dialog", { name: /Slack/ })).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();
    expect(
      (
        screen.getByRole("radio", { name: "All tools" }) as HTMLButtonElement
      ).getAttribute("data-state"),
    ).toBe("unchecked");
    fireEvent.click(screen.getByRole("radio", { name: "Specific tools" }));
    expect(allowSelectors(onChange.mock.calls.at(-1)?.[0])).toEqual([
      { resourceKind: "mcp", resourceId: "slack", tool: "search" },
    ]);
  });

  it("pins the Default project first wherever the inventory lists it", () => {
    inventory.groups = [...inventory.groups].reverse();
    renderPanel();
    const headings = screen
      .getAllByRole("heading", { level: 3 })
      .map((heading) => heading.textContent);
    expect(headings.slice(0, 2)).toEqual(["Default", "Data Platform"]);
  });

  it("searches a server's tools in the sheet", () => {
    renderPanel({
      "mcp:connect": connect([
        { resourceKind: "mcp", resourceId: "linear", tool: "search" },
      ]),
    });
    fireEvent.click(screen.getByRole("button", { name: "1 Tool" }));
    fireEvent.change(screen.getByPlaceholderText("Search tools"), {
      target: { value: "zzz" },
    });
    expect(screen.getByText(/No tools match/)).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText("Search tools"), {
      target: { value: "srch" },
    });
    expect(screen.queryByText(/No tools match/)).toBeNull();
  });
});
