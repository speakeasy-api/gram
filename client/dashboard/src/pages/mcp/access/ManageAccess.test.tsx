import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// What the page sends when a row changes. The rules the surface writes are the
// whole point, so the tests assert the payload rather than the rendering alone.
const { mutate } = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@gram/client/react-query/setResourceAudience.js", () => ({
  useSetResourceAudienceMutation: () => ({ mutate, isPending: false }),
}));

vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: vi.fn(),
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

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
}));

vi.mock("react-router", () => ({
  useNavigate: () => vi.fn(),
}));

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    access: { roles: { href: () => "/org/access/roles" } },
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasAnyScope: () => true }),
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
beforeEach(() => mutate.mockClear());

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
  fireEvent.click(screen.getAllByRole("button", { expanded: false })[0]!);
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

  it("says a personal grant a role's block cancels is not in force", () => {
    // Blocks reach people, not principals: the block on Engineering takes
    // connect from everyone in it, including Hana's own rule. Showing
    // "Search" here would promise access the server refuses.
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

    // The role's own row says it too: the block covers every server, so it
    // is not that row's to lift either.
    expect(
      screen.getAllByText("blocked by Engineering").length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText("Search")).toBeNull();
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
