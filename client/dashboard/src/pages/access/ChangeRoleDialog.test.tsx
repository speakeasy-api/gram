import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { ScopeDefinition } from "@gram/client/models/components/scopedefinition.js";
import type { ReactNode } from "react";

import { ChangeRoleDialog } from "./ChangeRoleDialog";
import { scopeCatalogue } from "./scopeCatalogue";

const state = vi.hoisted(() => ({
  update: vi.fn(),
  refetchScopes: vi.fn(),
  scopes: "ready" as "ready" | "loading" | "error",
}));
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
vi.mock("@gram/client/react-query/explainResourceAccess.js", () => ({
  invalidateAllExplainResourceAccess: vi.fn(),
}));
vi.mock("@gram/client/react-query/identityAccess.js", () => ({
  invalidateAllIdentityAccess: vi.fn(),
}));
vi.mock("@gram/client/react-query/resourceAudience.js", () => ({
  invalidateAllResourceAudience: vi.fn(),
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  invalidateAllMembers: vi.fn(),
}));
vi.mock("@gram/client/react-query/roles.js", () => ({
  invalidateAllRoles: vi.fn(),
  useRoles: () => ({ data: { roles: [builder, auditor] } }),
}));
vi.mock("@gram/client/react-query/updateMemberRoles.js", () => ({
  useUpdateMemberRolesMutation: () => ({
    mutate: state.update,
    isPending: false,
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
const builder = {
  id: "builder",
  name: "Builder",
  description: "",
  principalUrn: "role:organization:builder",
  grants: [],
  isSystem: false,
  memberCount: 1,
} as unknown as Role;
const auditor = {
  id: "auditor",
  name: "Auditor",
  description: "",
  principalUrn: "role:organization:auditor",
  grants: [{ scope: "log:read" }],
  isSystem: false,
  memberCount: 0,
} as unknown as Role;
const member = {
  id: "member-1",
  name: "Ada Lovelace",
  email: "ada@example.com",
  roleIds: ["builder"],
  directoryRoleIds: [],
} as unknown as AccessMember;

function renderDialog() {
  return render(
    <ChangeRoleDialog member={member} onOpenChange={vi.fn<() => void>()} />,
  );
}
function addAuditor() {
  fireEvent.click(screen.getByText("Auditor"));
}
function updateButton(): HTMLButtonElement {
  return screen.getByRole("button", {
    name: "Update Roles",
  }) as HTMLButtonElement;
}

afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.scopes = "ready";
});

describe("adding a role that gives sensitive access", () => {
  it("names what the role adds and waits for the acknowledgement", () => {
    renderDialog();
    addAuditor();
    expect(
      screen.getByText("These roles add 1 permission, 1 is sensitive:"),
    ).toBeTruthy();
    expect(updateButton().disabled).toBe(true);

    fireEvent.click(
      screen.getByRole("checkbox", { name: "Confirm sensitive permissions" }),
    );
    expect(updateButton().disabled).toBe(false);
    fireEvent.click(updateButton());
    expect(state.update).toHaveBeenCalledOnce();
    expect(
      state.update.mock.calls[0]![0].request.updateMemberRolesForm,
    ).toEqual({ userId: "member-1", roleIds: ["builder", "auditor"] });
  });
});

describe("while the permission catalogue is unavailable", () => {
  // Sensitivity is read off the catalogue, so with nothing loaded the new
  // role would look safe and the update would go through unacknowledged.
  it("keeps the update disabled while the catalogue loads", () => {
    state.scopes = "loading";
    renderDialog();
    addAuditor();
    expect(screen.getByText("Loading permissions…")).toBeTruthy();
    expect(updateButton().disabled).toBe(true);
  });

  it("keeps the update disabled and lets the catalogue be fetched again after it failed", () => {
    state.scopes = "error";
    renderDialog();
    addAuditor();
    expect(
      screen.getByText(
        "Failed to load permissions, so roles can't be changed yet.",
      ),
    ).toBeTruthy();
    expect(updateButton().disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.refetchScopes).toHaveBeenCalledOnce();
  });
});
