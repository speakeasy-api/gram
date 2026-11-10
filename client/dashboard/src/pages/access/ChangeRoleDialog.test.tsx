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
  // The catalogue as last read; a test can have it read again with more.
  definitions: [] as ScopeDefinition[],
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
  useRoles: () => ({
    data: {
      roles: [
        builder,
        auditor,
        reviewer,
        oneProject,
        otherProject,
        wide,
        admin,
        blocker,
      ],
    },
  }),
}));
vi.mock("@gram/client/react-query/updateMemberRoles.js", () => ({
  useUpdateMemberRolesMutation: () => ({
    mutate: state.update,
    isPending: false,
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
  {
    slug: "org:admin" as ScopeDefinition["slug"],
    label: "Manage organization",
    description: "Manage organization settings, members, roles and access.",
    resourceType: "org",
    category: "org" as ScopeDefinition["category"],
    visibility: "user_visible",
    agentEligible: false,
    sensitive: true,
    subScopes: [],
    exclusionScope: "org:blocked_admin" as ScopeDefinition["exclusionScope"],
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
const otherProject = {
  id: "other-project",
  name: "Other project",
  description: "",
  principalUrn: "role:organization:other-project",
  grants: [
    {
      scope: "log:read",
      selectors: [{ resourceKind: "project", resourceId: "p2" }],
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
const admin = {
  id: "admin",
  name: "Admin",
  description: "",
  principalUrn: "role:organization:admin",
  grants: [{ scope: "org:admin" }],
  isSystem: false,
  memberCount: 0,
} as unknown as Role;
// Withholds the admin role's org:admin from whoever holds both.
const blocker = {
  id: "blocker",
  name: "Blocker",
  description: "",
  principalUrn: "role:organization:blocker",
  grants: [{ scope: "org:blocked_admin" }],
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

function renderDialog(roleIds: string[] = ["builder"]) {
  return render(
    <ChangeRoleDialog
      member={{ ...member, roleIds } as AccessMember}
      onOpenChange={vi.fn<() => void>()}
    />,
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
  state.definitions = definitions;
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
      screen.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    expect(updateButton().disabled).toBe(false);
    fireEvent.click(updateButton());
    expect(state.update).toHaveBeenCalledOnce();
    expect(
      state.update.mock.calls[0]![0].request.updateMemberRolesForm,
    ).toEqual({ userId: "member-1", roleIds: ["builder", "auditor"] });
  });
});

describe("adding a role that widens a permission the member holds", () => {
  it("asks for the acknowledgement when one project's tool logs become every project's", () => {
    renderDialog(["one-project"]);
    fireEvent.click(screen.getByText("Wide"));
    expect(
      screen.getByText("These roles add 1 permission, 1 is sensitive:"),
    ).toBeTruthy();
    expect(updateButton().disabled).toBe(true);
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    expect(updateButton().disabled).toBe(false);
  });

  it("asks for the acknowledgement when a second project's tool logs are added to one project's", () => {
    renderDialog(["one-project"]);
    fireEvent.click(screen.getByText("Other project"));
    expect(
      screen.getByText("These roles add 1 permission, 1 is sensitive:"),
    ).toBeTruthy();
    expect(updateButton().disabled).toBe(true);
  });

  it("needs none for a narrower grant of a permission held everywhere", () => {
    renderDialog(["wide"]);
    fireEvent.click(screen.getByText("One project"));
    expect(screen.queryByText("These roles add", { exact: false })).toBeNull();
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(updateButton().disabled).toBe(false);
  });
});

describe("after the sensitive permissions to acknowledge change", () => {
  it("asks again when the catalogue read again marks more of them sensitive", () => {
    // A fresh element each time, or React skips the render it would bail
    // out of for an element it has seen.
    const onOpenChange = vi.fn<() => void>();
    const current = { ...member, roleIds: ["builder"] } as AccessMember;
    const tree = () => (
      <ChangeRoleDialog member={current} onOpenChange={onOpenChange} />
    );
    const { rerender } = render(tree());
    fireEvent.click(screen.getByText("Reviewer"));
    expect(
      screen.getByText("These roles add 2 permissions, 1 is sensitive:"),
    ).toBeTruthy();
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    expect(updateButton().disabled).toBe(false);

    // The catalogue is read again and now says chat:read is sensitive too:
    // the acknowledgement given was of log:read alone.
    state.definitions = [...definitions, chatRead];
    rerender(tree());
    expect(
      screen.getByText("These roles add 2 permissions, 2 are sensitive:"),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("checkbox", { name: /I reviewed the sensitive permissions/ })
        .getAttribute("data-state"),
    ).toBe("unchecked");
    expect(updateButton().disabled).toBe(true);
    fireEvent.click(updateButton());
    expect(state.update).not.toHaveBeenCalled();
  });
});

describe("removing a role that withheld a permission", () => {
  /** The remove button on a selected role's chip: the badge's text's sibling. */
  function removeChip(roleName: string): HTMLElement {
    const button = screen
      .getByText(roleName)
      .parentElement?.querySelector("button");
    if (!button) throw new Error(`no remove button for ${roleName}`);
    return button;
  }

  it("asks for the acknowledgement when the exception on org:admin is lifted", () => {
    renderDialog(["admin", "blocker"]);
    fireEvent.click(removeChip("Blocker"));
    expect(
      screen.getByText("These roles add 1 permission, 1 is sensitive:"),
    ).toBeTruthy();
    expect(updateButton().disabled).toBe(true);
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /I reviewed the sensitive permissions/,
      }),
    );
    expect(updateButton().disabled).toBe(false);
    fireEvent.click(updateButton());
    expect(
      state.update.mock.calls[0]![0].request.updateMemberRolesForm,
    ).toEqual({ userId: "member-1", roleIds: ["admin"] });
  });

  it("needs none while the exception stays", () => {
    renderDialog(["admin", "blocker"]);
    addAuditor();
    expect(
      screen.getByText("These roles add 1 permission, 1 is sensitive:"),
    ).toBeTruthy();
    expect(screen.getByText("View tool logs")).toBeTruthy();
    expect(screen.queryByText("Manage organization")).toBeNull();
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
