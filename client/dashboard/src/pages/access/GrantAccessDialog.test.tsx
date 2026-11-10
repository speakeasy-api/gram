import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { ReactNode } from "react";

import { GrantAccessDialog } from "./GrantAccessDialog";
import { scopeCatalogue } from "./scopeCatalogue";

const state = vi.hoisted(() => ({
  assign: vi.fn(),
  refetchScopes: vi.fn(),
  scopes: "ready" as "ready" | "loading" | "error",
  memberRoleIds: [] as string[],
  // The catalogue as last read; a test can have it read again with more.
  definitions: [] as ScopeDefinition[],
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: vi.fn(),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  invalidateAllMembers: vi.fn(),
  useMembers: () => ({
    data: { members: [{ ...member, roleIds: state.memberRoleIds }] },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  invalidateAllRoles: vi.fn(),
  useRoles: () => ({
    data: { roles: [auditor, reviewer, oneProject, wide] },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/updateMemberRoles.js", () => ({
  useUpdateMemberRolesMutation: () => ({
    mutate: state.assign,
    isPending: false,
    isSuccess: false,
    isError: false,
  }),
}));
vi.mock("@/hooks/useScopeCatalogue", () => ({
  useScopeCatalogue: () => ({
    catalogue: scopeCatalogue(
      state.scopes === "ready" ? state.definitions : [],
    ),
    tasks: [],
    isLoading: state.scopes === "loading",
    isError: state.scopes === "error",
    refetch: state.refetchScopes,
  }),
}));
vi.mock("./CreateRoleDialog", () => ({ CreateRoleDialog: () => null }));
vi.mock("@/components/role-link", () => ({
  RoleLink: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

// A scope the catalogue can gain on a later read.
const chatRead: ScopeDefinition = {
  slug: "chat:read" as ScopeDefinition["slug"],
  label: "View agent sessions",
  description: "Read other members' agent session transcripts.",
  resourceType: "chat",
  category: "session" as ScopeDefinition["category"],
  visibility: "user_visible",
  agentEligible: false,
  sensitive: true,
  subScopes: [],
};

const definitions: ScopeDefinition[] = [
  {
    slug: "log:read" as ScopeDefinition["slug"],
    label: "View tool logs",
    description: "Read tool arguments and results.",
    resourceType: "project",
    category: "log" as ScopeDefinition["category"],
    visibility: "user_visible",
    agentEligible: true,
    sensitive: true,
    subScopes: [],
  },
];
const member = {
  id: "member-1",
  name: "Ada Lovelace",
  email: "ada@example.com",
  roleIds: [],
  directoryRoleIds: [],
} as unknown as AccessMember;
const auditor = {
  id: "auditor",
  name: "Auditor",
  description: "",
  principalUrn: "role:organization:auditor",
  grants: [{ scope: "log:read" }],
  isSystem: false,
  memberCount: 0,
} as unknown as Role;
const reviewer = {
  id: "reviewer",
  name: "Reviewer",
  description: "",
  principalUrn: "role:organization:reviewer",
  grants: [{ scope: "log:read" }, { scope: "chat:read" }],
  isSystem: false,
  memberCount: 0,
} as unknown as Role;
const oneProject = {
  id: "one-project",
  name: "One project",
  description: "",
  principalUrn: "role:organization:one-project",
  grants: [
    {
      scope: "log:read",
      selectors: [{ resourceKind: "project", resourceId: "p1" }],
    },
  ],
  isSystem: false,
  memberCount: 0,
} as unknown as Role;
const wide = {
  id: "wide",
  name: "Wide",
  description: "",
  principalUrn: "role:organization:wide",
  // The server stores an unrestricted grant as a wildcard selector.
  grants: [
    {
      scope: "log:read",
      selectors: [{ resourceKind: "project", resourceId: "*" }],
    },
  ],
  isSystem: false,
  memberCount: 0,
} as unknown as Role;

function renderDialog() {
  return render(
    <GrantAccessDialog
      userId={member.id}
      scope="log:read"
      onClose={vi.fn<() => void>()}
    />,
  );
}
/** The Assign button on one suggested role's row. */
function assignButton(roleName: string): HTMLElement {
  const row = screen.getByText(roleName).closest("li");
  if (!row) throw new Error(`no row for ${roleName}`);
  const button = row.querySelector("button");
  if (!button) throw new Error(`no Assign button for ${roleName}`);
  return button;
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.scopes = "ready";
  state.memberRoleIds = [];
  state.definitions = definitions;
});

describe("assigning a role that adds sensitive access", () => {
  it("previews what the role adds and waits for the acknowledgement before assigning", () => {
    renderDialog();
    fireEvent.click(assignButton("Auditor"));
    expect(state.assign).not.toHaveBeenCalled();
    expect(
      screen.getByText("This role adds 1 permission, 1 is sensitive:"),
    ).toBeTruthy();
    expect(
      screen.getByText("Assign “Auditor” to Ada Lovelace", { exact: false }),
    ).toBeTruthy();

    const assign = screen.getByRole("button", {
      name: "Assign role",
    }) as HTMLButtonElement;
    expect(assign.disabled).toBe(true);
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    expect(assign.disabled).toBe(false);
    fireEvent.click(assign);
    expect(state.assign).toHaveBeenCalledOnce();
    expect(
      state.assign.mock.calls[0]![0].request.updateMemberRolesForm,
    ).toEqual({ userId: "member-1", roleIds: ["auditor"] });
  });

  it("asks when the role gives everywhere what the member holds on one project", () => {
    state.memberRoleIds = ["one-project"];
    renderDialog();
    fireEvent.click(assignButton("Wide"));
    expect(state.assign).not.toHaveBeenCalled();
    expect(
      screen.getByText("This role adds 1 permission, 1 is sensitive:"),
    ).toBeTruthy();
  });

  it("asks again when the catalogue read again marks more of what the role adds sensitive", () => {
    // A fresh element each time, or React skips the render it would bail
    // out of for an element it has seen.
    const onClose = vi.fn<() => void>();
    const tree = () => (
      <GrantAccessDialog
        userId={member.id}
        scope="log:read"
        onClose={onClose}
      />
    );
    const { rerender } = render(tree());
    fireEvent.click(assignButton("Reviewer"));
    expect(
      screen.getByText("This role adds 2 permissions, 1 is sensitive:"),
    ).toBeTruthy();
    const assign = () =>
      screen.getByRole("button", { name: "Assign role" }) as HTMLButtonElement;
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    expect(assign().disabled).toBe(false);

    // The catalogue is read again and now says chat:read is sensitive too:
    // the acknowledgement given was of log:read alone.
    state.definitions = [...definitions, chatRead];
    rerender(tree());
    expect(
      screen.getByText("This role adds 2 permissions, 2 are sensitive:"),
    ).toBeTruthy();
    expect(assign().disabled).toBe(true);
    fireEvent.click(assign());
    expect(state.assign).not.toHaveBeenCalled();
  });

  it("assigns straight away when the member already holds the permission everywhere", () => {
    state.memberRoleIds = ["wide"];
    renderDialog();
    fireEvent.click(assignButton("Auditor"));
    expect(screen.queryByRole("button", { name: "Assign role" })).toBeNull();
    expect(state.assign).toHaveBeenCalledOnce();
    expect(
      state.assign.mock.calls[0]![0].request.updateMemberRolesForm,
    ).toEqual({ userId: "member-1", roleIds: ["wide", "auditor"] });
  });
});

describe("while the permission catalogue is unavailable", () => {
  // Sensitivity is read off the catalogue, so with nothing loaded every role
  // would look safe to assign. Nothing is offered until it has loaded.
  it("offers no role while the catalogue loads", () => {
    state.scopes = "loading";
    renderDialog();
    expect(screen.queryByRole("button", { name: "Assign" })).toBeNull();
  });

  it("offers no role and lets the catalogue be fetched again after it failed", () => {
    state.scopes = "error";
    renderDialog();
    expect(screen.queryByRole("button", { name: "Assign" })).toBeNull();
    expect(
      screen.getByText("Failed to load members, roles and permissions."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchScopes).toHaveBeenCalledOnce();
  });
});
