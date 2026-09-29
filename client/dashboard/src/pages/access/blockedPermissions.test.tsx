import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { CreateRoleDialog } from "./CreateRoleDialog";
import type { Role } from "@gram/client/models/components/role.js";

/**
 * A role that holds a block and not the permission it subtracts from — what
 * revoking one server's access leaves behind when a member reaches that server
 * through another role. The editor folds the block onto the permission to say
 * "all servers except one" in a single sentence, and with no allow beside it
 * that fold used to render as the permission itself, with no control on the
 * row: `mcp:blocked_read` read as `mcp:read` and could not be touched.
 */

const mocks = vi.hoisted(() => ({
  update: vi.fn(),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetryContext: () => ({
    featureFlags: { status: "ready" },
    telemetry: { isFeatureEnabled: () => false },
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ projects: [], scimEnabled: false }),
}));
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ identity: { href: () => "/org/identity" } }),
}));
vi.mock("@gram/client/react-query/agents.js", () => ({
  useAgents: () => ({ data: [] }),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => ({ data: { members: [] } }),
  invalidateAllMembers: vi.fn(),
}));
vi.mock("@gram/client/react-query/listScopes.js", () => ({
  useListScopes: () => ({
    data: {
      scopes: [
        {
          slug: "mcp:read",
          description: "View MCP servers and configuration.",
          resourceType: "mcp",
          visibility: "user_visible",
          agentEligible: true,
          exclusionScope: "mcp:blocked_read",
        },
        {
          slug: "mcp:connect",
          description: "Connect to and use MCP servers.",
          resourceType: "mcp",
          visibility: "user_visible",
          agentEligible: true,
          exclusionScope: "mcp:blocked_connect",
        },
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/createRole.js", () => ({
  useCreateRoleMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@gram/client/react-query/updateRole.js", () => ({
  useUpdateRoleMutation: () => ({ mutate: mocks.update, isPending: false }),
}));

// The picker and the row menus are Radix surfaces that only mount once opened.
// Flattened here so a click can reach an item without driving the animation.
vi.mock("@/components/ui/Popover", () => ({
  Popover: ({ children }: { children: ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ children }: { children: ReactNode }) => <>{children}</>,
  PopoverContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
}));
vi.mock("@/components/ui/Command", () => ({
  Command: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  CommandEmpty: () => null,
  CommandGroup: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  CommandInput: () => null,
  CommandList: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  CommandItem: ({
    children,
    onSelect,
  }: {
    children: ReactNode;
    onSelect?: () => void;
  }) => (
    <div role="option" aria-selected={false} onClick={onSelect}>
      {children}
    </div>
  ),
}));
vi.mock("@/components/ui/Dropdown", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ children }: { children: ReactNode }) => (
    <>{children}</>
  ),
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuSeparator: () => null,
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: ReactNode;
    onClick?: () => void;
  }) => (
    <div role="menuitem" onClick={onClick}>
      {children}
    </div>
  ),
}));
vi.mock("./GrantRuleDrawerContent", () => ({
  GrantRuleDrawerContent: ({
    onChangeSelectors,
  }: {
    onChangeSelectors: (
      selectors: Array<{ resourceKind: string; resourceId: string }>,
    ) => void;
  }) => (
    <button
      onClick={() =>
        onChangeSelectors([{ resourceKind: "mcp", resourceId: "srv_2" }])
      }
    >
      Select server 2
    </button>
  ),
}));

function roleBlocking(
  selectors?: { resourceKind: string; resourceId: string }[],
) {
  return {
    id: "role-test",
    principalUrn: "role:organization:role-test",
    name: "FTE",
    slug: "org-fte",
    description: "FTE role",
    isSystem: false,
    memberCount: 0,
    grants: [{ scope: "mcp:blocked_read", selectors }],
    createdAt: new Date("2026-09-29T00:00:00Z"),
    updatedAt: new Date("2026-09-29T00:00:00Z"),
  } as Role;
}

function renderEditor(editingRole: Role) {
  return render(
    <MemoryRouter>
      <QueryClientProvider client={new QueryClient()}>
        <CreateRoleDialog
          open
          onOpenChange={vi.fn<(open: boolean) => void>()}
          editingRole={editingRole}
          presentation="page"
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

/** The row for a permission, found by the dismiss button naming it. */
function row(slug: string): HTMLElement {
  const dismiss = screen.getByRole("button", { name: `Remove ${slug}` });
  const container = dismiss.closest("div.flex.items-start");
  expect(container).not.toBeNull();
  return container as HTMLElement;
}

function save() {
  fireEvent.click(screen.getByRole("button", { name: "Save Changes" }));
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
});

it("names the block rather than the permission it subtracts from", () => {
  renderEditor(roleBlocking([{ resourceKind: "mcp", resourceId: "srv_1" }]));

  const blocked = row("mcp:blocked_read");
  expect(blocked.textContent).toContain("Blocked");
  expect(blocked.textContent).toContain(
    "Blocks this access even where another role allows it.",
  );
  // The permission's own description would claim access the role never gives.
  expect(blocked.textContent).not.toContain(
    "View MCP servers and configuration.",
  );
  expect(screen.queryByRole("button", { name: "Remove mcp:read" })).toBeNull();
});

it("states what the block covers, and lets it be re-pointed and saved", () => {
  renderEditor(roleBlocking([{ resourceKind: "mcp", resourceId: "srv_1" }]));

  expect(row("mcp:blocked_read").textContent).toContain("Excludes");
  expect(row("mcp:blocked_read").textContent).toContain("1 server");

  fireEvent.click(screen.getByRole("menuitem", { name: "Specific servers…" }));
  fireEvent.click(screen.getByRole("button", { name: "Select server 2" }));
  fireEvent.click(screen.getByRole("button", { name: "Done" }));
  save();

  expect(mocks.update).toHaveBeenCalledOnce();
  const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
  expect(form.addGrants).toEqual([
    {
      scope: "mcp:blocked_read",
      selectors: [{ resourceKind: "mcp", resourceId: "srv_2" }],
    },
  ]);
  expect(form.removeGrants).toEqual([
    {
      scope: "mcp:blocked_read",
      selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
    },
  ]);
});

it("widens a narrowed block back to everything", () => {
  renderEditor(roleBlocking([{ resourceKind: "mcp", resourceId: "srv_1" }]));

  fireEvent.click(screen.getByRole("menuitem", { name: "All servers" }));
  expect(row("mcp:blocked_read").textContent).toContain("All servers");
  save();

  const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
  expect(form.addGrants).toEqual([
    { scope: "mcp:blocked_read", selectors: undefined },
  ]);
  expect(form.removeGrants).toEqual([
    {
      scope: "mcp:blocked_read",
      selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
    },
  ]);
});

it("takes the whole block off when the row is dismissed", () => {
  renderEditor(roleBlocking([{ resourceKind: "mcp", resourceId: "srv_1" }]));

  fireEvent.click(
    screen.getByRole("button", { name: "Remove mcp:blocked_read" }),
  );
  save();

  const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
  expect(form.addGrants).toEqual([]);
  expect(form.removeGrants).toEqual([
    {
      scope: "mcp:blocked_read",
      selectors: [{ resourceKind: "mcp", resourceId: "srv_1" }],
    },
  ]);
});

it("offers the blocked permission as one the role does not have yet", () => {
  renderEditor(roleBlocking([{ resourceKind: "mcp", resourceId: "srv_1" }]));

  // A tick would say the role already reads MCP servers; it only blocks them.
  const option = screen.getByRole("option", { name: /^mcp:read/ });
  expect(option.textContent).not.toContain("Already added");

  // Picking it grants the permission and leaves the narrower block standing.
  fireEvent.click(option);
  expect(row("mcp:read").textContent).toContain("Applies to");
  expect(screen.getByRole("button", { name: "Remove exception" })).toBeTruthy();
  save();

  const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
  expect(form.addGrants).toEqual([{ scope: "mcp:read", selectors: undefined }]);
  expect(form.removeGrants).toEqual([]);
});

it("drops a block covering everything when the permission is added", () => {
  // Stored as the server's wildcard selector, which the editor reads back as
  // unrestricted. Kept beside an allow it would cancel it outright.
  renderEditor(roleBlocking([{ resourceKind: "mcp", resourceId: "*" }]));

  expect(row("mcp:blocked_read").textContent).toContain("All servers");
  fireEvent.click(screen.getByRole("option", { name: /^mcp:read/ }));

  expect(row("mcp:read").textContent).toContain("Applies to");
  expect(screen.queryByRole("button", { name: "Remove exception" })).toBeNull();
  save();

  const form = mocks.update.mock.calls[0]![0].request.updateRoleForm;
  expect(form.addGrants).toEqual([{ scope: "mcp:read", selectors: undefined }]);
  expect(form.removeGrants).toEqual([
    { scope: "mcp:blocked_read", selectors: undefined },
  ]);
});
