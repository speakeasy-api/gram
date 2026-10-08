import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// What the page sends when a row changes. The rules the surface writes are the
// whole point, so the tests assert the payload rather than the rendering alone.
const { mutate, mutationOptions, invalidate, permission } = vi.hoisted(() => ({
  mutate: vi.fn(),
  mutationOptions: vi.fn(),
  invalidate: vi.fn(),
  permission: { admin: true },
}));

vi.mock("@gram/client/react-query/setResourceAudience.js", () => ({
  useSetResourceAudienceMutation: (options: unknown) => {
    mutationOptions(options);
    return { mutate, isPending: false };
  },
}));

vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: vi.fn(),
}));

vi.mock("@gram/client/react-query/explainResourceAccess.js", () => ({
  invalidateAllExplainResourceAccess: invalidate,
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  invalidateAllRoles: invalidate,
}));
vi.mock("@gram/client/react-query/plugin.js", () => ({
  invalidateAllPlugin: invalidate,
}));
vi.mock("@gram/client/react-query/plugins.js", () => ({
  invalidateAllPlugins: invalidate,
}));

vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({
    data: {
      members: [
        {
          id: "user-1",
          name: "Hana Sato",
          email: "hana@example.com",
          photoUrl: undefined,
        },
      ],
    },
  }),
}));

vi.mock("@gram/client/react-query/audienceOptions.js", () => ({
  useAudienceOptions: () => ({
    data: {
      options: [
        {
          principalUrn: "role:global:1",
          kind: "role",
          displayName: "Engineering",
        },
        {
          principalUrn: "agent:agent-1",
          kind: "agent",
          displayName: "Release Bot",
          description: "Ships releases",
        },
      ],
    },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

vi.mock("@/routes", () => ({
  useRoutes: () => ({
    plugins: { detail: { href: (id: string) => `/org/plugins/${id}` } },
  }),
  useOrgRoutes: () => ({
    plugins: { detail: { href: (id: string) => `/org/plugins/${id}` } },
    access: { roles: { href: () => "/org/access/roles" } },
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => permission.admin }),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

vi.mock("@/components/identity-link", () => ({
  IdentityLink: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

import { TooltipProvider } from "@/components/ui/Tooltip";
import { ManageAccess } from "./ManageAccess";

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  permission.admin = true;
});

function entry(
  overrides: Partial<ResourceAudienceEntry>,
): ResourceAudienceEntry {
  return {
    principalUrn: "user:1",
    kind: "user",
    displayName: "Hana Sato",
    level: "use",
    appliesTo: "resource",
    ...overrides,
  } as ResourceAudienceEntry;
}

function renderList(entries: ResourceAudienceEntry[]) {
  return render(
    // The facepile's avatars carry tooltips, which the app provides at the
    // root.
    <TooltipProvider>
      <ManageAccess
        resourceId="server-1"
        resourceName="Acme Ops"
        entries={entries}
        version="v1"
        isLoading={false}
      />
    </TooltipProvider>,
  );
}

/** The rules the last save would have written. */
function savedEntries() {
  const [variables] = mutate.mock.calls.at(-1) ?? [];
  return variables.request.setResourceAudienceForm;
}

/** Open a collapsed row so its per-scope lines become reachable. */
function expandFirstRow() {
  // Named rather than taken by position: the "Grant access" menu is also a
  // collapsed button, and it comes first in the DOM.
  fireEvent.click(screen.getAllByRole("button", { name: "Edit access" })[0]!);
}

/**
 * Whether the row's controls are hidden. A collapsed row keeps them in the
 * DOM so the disclosure can animate, and marks them inert instead.
 */
function controlsHidden() {
  return (
    screen
      .getByText("Connect")
      .closest("[aria-hidden]")
      ?.getAttribute("aria-hidden") === "true"
  );
}

describe("the access list", () => {
  it("reads a row at a glance before it is opened", () => {
    renderList([entry({ tools: ["search"] })]);

    expect(screen.getByText("Hana Sato")).toBeTruthy();
    // Collapsed: one line saying what this principal can do here, and the
    // per-scope controls out of reach until the row is opened.
    expect(screen.getAllByText("Search").length).toBeGreaterThan(0);
    expect(controlsHidden()).toBe(true);
  });

  it("gives every principal a line for each scope once opened", () => {
    renderList([entry({ tools: ["search"] })]);
    expandFirstRow();

    for (const label of ["Connect", "View", "Manage"]) {
      expect(screen.getByText(label)).toBeTruthy();
    }
    // Connect reaches individual tools; the other two are the server itself.
    expect(screen.getAllByText("No access")).toHaveLength(2);
  });

  it("reads a weaker scope as granted by the stronger one that satisfies it", () => {
    renderList([entry({ level: "manage" })]);
    expandFirstRow();

    // Manage satisfies the connect and read checks, so all three lines read
    // as open — without explaining which rule did it.
    expect(screen.getAllByText("All tools").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Allowed")).toHaveLength(2);
    expect(screen.queryByText("via Manage")).toBeNull();
  });

  it("shows a role's members as faces rather than a count", () => {
    renderList([
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Engineering",
        appliesTo: "all_resources",
        description: "1 member",
        memberIds: ["user-1"],
      }),
    ]);

    // The faces answer "who does this reach"; the count does not.
    expect(screen.getByText("HS")).toBeTruthy();
    expect(screen.queryByText("1 member")).toBeNull();
  });

  it("shows a dash when a role reaches nobody yet", () => {
    renderList([
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Engineering",
        appliesTo: "all_resources",
        description: "0 members",
        memberIds: [],
      }),
    ]);

    // The column is for faces; a count is not one, and neither is an email.
    expect(screen.queryByText("0 members")).toBeNull();
    expect(screen.getByText("—")).toBeTruthy();
  });

  it("marks a role as a role, so it does not read as a person", () => {
    renderList([
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Engineering",
        appliesTo: "all_resources",
      }),
    ]);

    // A badge, not a guess from the name.
    expect(screen.getByText("Role")).toBeTruthy();
    // The link to the role editor belongs with the controls it complements.
    expect(controlsHidden()).toBe(true);
    expandFirstRow();
    expect(controlsHidden()).toBe(false);
    expect(screen.getByText("Edit in Role Manager")).toBeTruthy();
  });

  it("lets a role reached by an organization-wide rule be edited here", () => {
    renderList([
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Engineering",
        appliesTo: "all_resources",
      }),
    ]);
    expandFirstRow();

    // The page is about one server, so the line says what the role has here
    // and not where the rule was written.
    expect(screen.queryByText("on every server")).toBeNull();
    // The row summary says it too, so the line is not the only match.
    expect(screen.getAllByText("All tools").length).toBeGreaterThan(0);
    expect(screen.getAllByText("No access")).toHaveLength(2);
  });

  it("lets a personal grant outrank a role's block", () => {
    // Hana's own rule names this server, so it outranks the block reaching
    // her through Engineering: the line shows what her rule opens, and no
    // note sends an administrator to the role.
    renderList([
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Engineering",
        appliesTo: "all_resources",
        level: "blocked",
        memberIds: ["user-1"],
      }),
      entry({ principalUrn: "user:user-1", tools: ["search"] }),
    ]);

    fireEvent.click(screen.getAllByRole("button", { expanded: false })[1]!);

    // The expanded Connect line itself, not the row summary above it.
    const connectLines = screen
      .getAllByText("Call this server's tools")
      .map((hint) => hint.parentElement!.parentElement!);
    expect(
      connectLines.some((line) => line.textContent?.includes("Search")),
    ).toBe(true);
    const notes = screen
      .queryAllByText(/blocked by/)
      .filter((el) => el.textContent === "blocked by Engineering");
    expect(notes).toHaveLength(0);
  });

  it("withholds agents a role's block reaches from the agent picker", () => {
    renderList([
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Engineering",
        appliesTo: "all_resources",
        level: "blocked",
        agentIds: ["agent-1"],
      }),
    ]);

    fireEvent.pointerDown(
      screen.getByRole("button", { name: /Grant access/ }),
      {
        button: 0,
        ctrlKey: false,
        pointerType: "mouse",
      },
    );
    fireEvent.click(screen.getByRole("menuitem", { name: "Agent" }));

    fireEvent.click(screen.getByRole("combobox"));

    expect(
      screen.getByText("Blocked by Engineering on this server"),
    ).toBeTruthy();
  });

  it("grants a picked role Connect through the existing audience endpoint", async () => {
    renderList([entry({})]);
    fireEvent.pointerDown(
      screen.getByRole("button", { name: /Grant access/ }),
      { button: 0, ctrlKey: false, pointerType: "mouse" },
    );
    fireEvent.click(screen.getByRole("menuitem", { name: "Role" }));
    expect(
      screen.getByRole("heading", { name: "Grant role access" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("combobox"));
    expect(screen.queryByText("Release Bot")).toBeNull();
    fireEvent.click(screen.getByRole("option", { name: /Engineering/ }));
    fireEvent.keyDown(screen.getByRole("option", { name: /Engineering/ }), {
      key: "Escape",
    });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    expect(savedEntries()).toEqual({
      resourceKind: "mcp",
      resourceId: "server-1",
      expectedVersion: "v1",
      entries: [
        { principalUrn: "user:1", level: "use" },
        { principalUrn: "role:global:1", level: "use" },
      ],
    });
    await mutationOptions.mock.calls.at(-1)![0].onSuccess();
    expect(invalidate).toHaveBeenCalledTimes(4);
  });

  it("says when nobody reaches the server", () => {
    renderList([]);
    expect(screen.getByText(/Nobody reaches/)).toBeTruthy();
  });
});

describe("what a row change writes", () => {
  it("removes one principal by leaving it out of the replacement", () => {
    renderList([
      entry({}),
      entry({ principalUrn: "user:2", displayName: "Jonas" }),
    ]);

    fireEvent.click(screen.getByLabelText("Remove Hana Sato"));

    const form = savedEntries();
    expect(
      form.entries.map((e: { principalUrn: string }) => e.principalUrn),
    ).toEqual(["user:2"]);
    expect(form.resourceId).toBe("server-1");
    expect(form.expectedVersion).toBe("v1");
  });

  it("keeps organization-level rules out of what it saves", () => {
    renderList([
      entry({}),
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Admin",
        appliesTo: "all_resources",
        level: "manage",
      }),
    ]);

    fireEvent.click(screen.getByLabelText("Remove Hana Sato"));

    expect(savedEntries().entries).toEqual([]);
  });

  it("subtracts a role an organization-wide rule still reaches", () => {
    // The rule is the role editor's, so taking the role off this one server
    // has to be written as a block naming it.
    renderList([
      entry({
        principalUrn: "role:global:1",
        kind: "role",
        displayName: "Engineering",
        appliesTo: "all_resources",
      }),
    ]);

    fireEvent.click(
      screen.getByLabelText("Remove Engineering from this server"),
    );
    // A block outranks every grant, so this one is confirmed before it lands.
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));

    // Blocks are independent, so taking the role off this server names all
    // three scopes.
    expect(
      savedEntries()
        .entries.map((e: { level: string }) => e.level)
        .sort(),
    ).toEqual(["blocked", "blocked_manage", "blocked_view"]);
  });
});

it("confirms removing an agent that remains covered by a broader role allow", () => {
  renderList([
    entry({ principalUrn: "agent:a1", kind: "agent", displayName: "Releaser" }),
    entry({
      principalUrn: "role:global:1",
      kind: "role",
      displayName: "Engineering",
      appliesTo: "all_resources",
      level: "manage",
      agentIds: ["a1"],
    }),
  ]);
  fireEvent.click(screen.getByLabelText("Remove Releaser from this server"));
  expect(mutate).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Remove" }));
  expect(
    savedEntries()
      .entries.map((e: { principalUrn: string; level: string }) => [
        e.principalUrn,
        e.level,
      ])
      .sort(),
  ).toEqual([
    ["agent:a1", "blocked"],
    ["agent:a1", "blocked_manage"],
    ["agent:a1", "blocked_view"],
  ]);
});

describe("role plugin distribution", () => {
  const role = entry({
    principalUrn: "role:global:1",
    kind: "role",
    displayName: "Engineering",
  });
  const plugin = {
    principalUrn: role.principalUrn,
    pluginId: "plugin-1",
    name: "Tools",
    slug: "tools",
  };
  const ui = (rolePlugins: (typeof plugin)[], entries = [role]) => (
    <TooltipProvider>
      <ManageAccess
        resourceId="server-1"
        entries={entries}
        version="v1"
        isLoading={false}
        rolePlugins={rolePlugins}
      />
    </TooltipProvider>
  );

  it("shows an empty distribution without inventing links", () => {
    render(ui([]));
    expect(screen.getByText("Distributed via")).toBeDefined();
    expect(screen.queryByRole("link", { name: "Tools" })).toBeNull();
  });

  it("links one exact role match to its plugin detail", () => {
    render(
      ui([
        plugin,
        {
          ...plugin,
          principalUrn: "role:other:1",
          pluginId: "wrong",
          name: "Unrelated",
        },
      ]),
    );
    expect(
      screen.getByRole("link", { name: "Tools" }).getAttribute("href"),
    ).toBe("/org/plugins/plugin-1");
    expect(screen.queryByText("Unrelated")).toBeNull();
  });

  it("sorts multiple plugins and disambiguates duplicate names with slugs", () => {
    render(
      ui([
        { ...plugin, pluginId: "z", slug: "z" },
        { ...plugin, pluginId: "a", slug: "a" },
        { ...plugin, pluginId: "first", name: "Alpha" },
      ]),
    );
    const links = screen
      .getAllByRole("link")
      .filter((link) => link.getAttribute("href")?.includes("/plugins/"));
    expect(links.map((link) => link.textContent)).toEqual([
      "Alpha",
      "Tools (a)",
      "Tools (z)",
    ]);
    expect(links[0]?.parentElement?.textContent).toBe(
      "Alpha, Tools (a), Tools (z)",
    );
  });

  it("does not show plugins on non-role rows", () => {
    render(ui([{ ...plugin, principalUrn: "user:1" }], [entry({})]));
    expect(screen.queryByRole("link", { name: "Tools" })).toBeNull();
  });

  it("hides cached plugin data immediately after permission downgrade", () => {
    const { rerender } = render(ui([plugin]));
    expect(screen.getByRole("link", { name: "Tools" })).toBeDefined();
    permission.admin = false;
    rerender(ui([plugin]));
    expect(screen.queryByRole("link", { name: "Tools" })).toBeNull();
    expect(screen.queryByText("Distributed via")).toBeNull();
    expect(screen.getByRole("link", { name: "Engineering" })).toBeDefined();
  });
});
