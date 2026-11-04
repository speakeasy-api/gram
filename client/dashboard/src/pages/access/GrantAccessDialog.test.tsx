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
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: vi.fn(),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  invalidateAllMembers: vi.fn(),
  useMembers: () => ({
    data: { members: [member] },
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
  }),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  invalidateAllRoles: vi.fn(),
  useRoles: () => ({
    data: { roles: [auditor] },
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
    catalogue: scopeCatalogue(state.scopes === "ready" ? definitions : []),
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

function renderDialog() {
  return render(
    <GrantAccessDialog userId={member.id} scope="log:read" onClose={vi.fn()} />,
  );
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.scopes = "ready";
});

describe("assigning a role that gives sensitive access", () => {
  it("previews the role and waits for confirmation before assigning", () => {
    renderDialog();
    fireEvent.click(screen.getByRole("button", { name: "Assign" }));
    expect(state.assign).not.toHaveBeenCalled();
    expect(
      screen.getByText("This role gives 1 permission, 1 is sensitive:"),
    ).toBeTruthy();
    expect(
      screen.getByText("Assign “Auditor” to Ada Lovelace", { exact: false }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Assign role" }));
    expect(state.assign).toHaveBeenCalledOnce();
    expect(
      state.assign.mock.calls[0]![0].request.updateMemberRolesForm,
    ).toEqual({ userId: "member-1", roleIds: ["auditor"] });
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
