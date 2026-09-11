import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  roles: {
    data: undefined as undefined | { roles: Array<Record<string, unknown>> },
    refetch: vi.fn(),
  },
  members: {
    data: undefined as undefined | { members: Array<Record<string, unknown>> },
  },
  createRole: vi.fn(),
  updateRole: vi.fn(),
  updateMemberRoles: vi.fn(),
  organization: { id: "org-one", scimEnabled: false as boolean },
  hasScope: vi.fn(),
  handleAPIError: vi.fn(),
  invalidateAllGrants: vi.fn(),
  invalidateAllRoles: vi.fn(),
  invalidateAllMembers: vi.fn(),
}));

vi.mock("@gram/client/react-query/roles.js", () => ({
  useRoles: () => mocks.roles,
  invalidateAllRoles: mocks.invalidateAllRoles,
}));
vi.mock("@gram/client/react-query/members.js", () => ({
  useMembers: () => mocks.members,
  invalidateAllMembers: mocks.invalidateAllMembers,
}));
vi.mock("@gram/client/react-query/grants.js", () => ({
  invalidateAllGrants: mocks.invalidateAllGrants,
}));
vi.mock("@gram/client/react-query/createRole.js", () => ({
  useCreateRoleMutation: () => ({
    mutateAsync: mocks.createRole,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/updateRole.js", () => ({
  useUpdateRoleMutation: () => ({
    mutateAsync: mocks.updateRole,
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/updateMemberRoles.js", () => ({
  useUpdateMemberRolesMutation: () => ({
    mutateAsync: mocks.updateMemberRoles,
    isPending: false,
  }),
}));
vi.mock("@/contexts/Auth", () => ({
  useUser: () => ({ id: "user-me" }),
  useOrganization: () => mocks.organization,
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
vi.mock("@/lib/errors", () => ({ handleAPIError: mocks.handleAPIError }));

import {
  SESSION_AUDITOR_ROLE_NAME,
  SESSION_AUDITOR_ROLE_SLUG,
  useSessionAuditAccess,
} from "./session-audit-access";

const UNRESTRICTED = { resourceKind: "chat", resourceId: "*" };
const AUDITOR_ROLE = {
  id: "role-auditor",
  slug: SESSION_AUDITOR_ROLE_SLUG,
  grants: [{ scope: "chat:read", selectors: [UNRESTRICTED] }],
};

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function conflict(): Error & { statusCode: number } {
  return Object.assign(new Error("role already exists"), { statusCode: 409 });
}

function setup() {
  return renderHook(() => useSessionAuditAccess(), { wrapper }).result;
}

beforeEach(() => {
  mocks.roles.data = { roles: [] };
  mocks.roles.refetch.mockReset().mockResolvedValue({ data: { roles: [] } });
  mocks.members.data = {
    members: [{ id: "user-me", roleIds: ["role-admin"] }],
  };
  mocks.createRole.mockReset().mockResolvedValue({});
  mocks.updateRole.mockReset().mockResolvedValue({});
  mocks.updateMemberRoles.mockReset().mockResolvedValue({});
  mocks.organization.scimEnabled = false;
  mocks.hasScope.mockReset().mockReturnValue(false);
  mocks.handleAPIError.mockReset();
  mocks.invalidateAllGrants.mockReset().mockResolvedValue(undefined);
  mocks.invalidateAllRoles.mockReset().mockResolvedValue(undefined);
  mocks.invalidateAllMembers.mockReset().mockResolvedValue(undefined);
});

describe("useSessionAuditAccess", () => {
  it("creates the role carrying chat:read with the caller on it", async () => {
    const result = setup();

    act(() => result.current.grant());

    await waitFor(() => expect(mocks.createRole).toHaveBeenCalled());
    expect(mocks.createRole.mock.calls[0]![0]).toEqual({
      request: {
        createRoleForm: {
          name: SESSION_AUDITOR_ROLE_NAME,
          description: expect.stringContaining("agent sessions"),
          grants: [{ scope: "chat:read", selectors: undefined }],
          memberIds: ["user-me"],
        },
      },
    });
    // Creation carries the membership, so there is nothing left to assign.
    expect(mocks.updateMemberRoles).not.toHaveBeenCalled();
    await waitFor(() => expect(mocks.invalidateAllGrants).toHaveBeenCalled());
  });

  it("assigns an existing role rather than creating a second one", async () => {
    mocks.roles.data = { roles: [AUDITOR_ROLE] };
    const result = setup();

    act(() => result.current.grant());

    await waitFor(() => expect(mocks.updateMemberRoles).toHaveBeenCalled());
    expect(mocks.createRole).not.toHaveBeenCalled();
    expect(mocks.updateMemberRoles.mock.calls[0]![0]).toEqual({
      request: {
        updateMemberRolesForm: {
          userId: "user-me",
          // Admin has to be sent back: the endpoint replaces the whole list.
          roleIds: ["role-admin", "role-auditor"],
        },
      },
    });
    // Its permissions are intact, so there is nothing to repair.
    expect(mocks.updateRole).not.toHaveBeenCalled();
  });

  it("restores chat:read on a reused role someone edited it off", async () => {
    mocks.roles.data = {
      roles: [{ ...AUDITOR_ROLE, grants: [{ scope: "mcp:read" }] }],
    };
    const result = setup();

    act(() => result.current.grant());

    await waitFor(() => expect(mocks.updateRole).toHaveBeenCalled());
    expect(mocks.updateRole.mock.calls[0]![0]).toEqual({
      request: {
        updateRoleForm: {
          id: "role-auditor",
          addGrants: [{ scope: "chat:read", selectors: undefined }],
        },
      },
    });
    // Repaired first, then assigned — never assigned as a role that reads
    // nothing.
    await waitFor(() => expect(mocks.updateMemberRoles).toHaveBeenCalled());
  });

  it("leaves a reused role alone when chat:write already covers the read", async () => {
    mocks.roles.data = {
      roles: [
        {
          ...AUDITOR_ROLE,
          grants: [{ scope: "chat:write", selectors: [UNRESTRICTED] }],
        },
      ],
    };
    const result = setup();

    act(() => result.current.grant());

    await waitFor(() => expect(mocks.updateMemberRoles).toHaveBeenCalled());
    expect(mocks.updateRole).not.toHaveBeenCalled();
  });

  it("restores an unrestricted grant when the role's chat:read was narrowed", async () => {
    // Reaches some sessions, but not necessarily the one the hook is about
    // to deliver — so it is no better than having none.
    mocks.roles.data = {
      roles: [
        {
          ...AUDITOR_ROLE,
          grants: [
            {
              scope: "chat:read",
              selectors: [{ resourceKind: "chat", resourceId: "chat-123" }],
            },
          ],
        },
      ],
    };
    const result = setup();
    expect(result.current.roleReadsSessions).toBe(false);

    act(() => result.current.grant());

    await waitFor(() => expect(mocks.updateRole).toHaveBeenCalled());
  });

  it("repairs the role that wins a creation race too", async () => {
    mocks.createRole.mockRejectedValue(conflict());
    mocks.roles.refetch.mockResolvedValue({
      data: {
        roles: [{ ...AUDITOR_ROLE, grants: [{ scope: "mcp:read" }] }],
      },
    });
    const result = setup();

    act(() => result.current.grant());

    await waitFor(() => expect(mocks.updateRole).toHaveBeenCalled());
    expect(mocks.updateRole.mock.calls[0]![0]).toMatchObject({
      request: { updateRoleForm: { id: "role-auditor" } },
    });
    await waitFor(() => expect(mocks.updateMemberRoles).toHaveBeenCalled());
  });

  it("assigns the winning role when another admin created it first", async () => {
    mocks.createRole.mockRejectedValue(conflict());
    mocks.roles.refetch.mockResolvedValue({ data: { roles: [AUDITOR_ROLE] } });
    const result = setup();

    act(() => result.current.grant());

    await waitFor(() => expect(mocks.updateMemberRoles).toHaveBeenCalled());
    expect(mocks.updateMemberRoles.mock.calls[0]![0]).toMatchObject({
      request: {
        updateMemberRolesForm: { roleIds: ["role-admin", "role-auditor"] },
      },
    });
    expect(mocks.handleAPIError).not.toHaveBeenCalled();
  });

  it("drops only the auditor role when handing access back", async () => {
    mocks.roles.data = { roles: [AUDITOR_ROLE] };
    mocks.members.data = {
      members: [
        {
          id: "user-me",
          roleIds: ["role-admin", "role-auditor", "role-other"],
        },
      ],
    };
    const result = setup();
    expect(result.current.holdsRole).toBe(true);

    act(() => result.current.revoke());

    await waitFor(() => expect(mocks.updateMemberRoles).toHaveBeenCalled());
    expect(mocks.updateMemberRoles.mock.calls[0]![0]).toEqual({
      request: {
        updateMemberRolesForm: {
          userId: "user-me",
          roleIds: ["role-admin", "role-other"],
        },
      },
    });
  });

  it("is unavailable until the caller's own membership record loads", () => {
    mocks.members.data = { members: [{ id: "someone-else", roleIds: [] }] };

    expect(setup().current.available).toBe(false);
  });

  it("is unavailable under directory sync", () => {
    mocks.organization.scimEnabled = true;

    const result = setup();
    expect(result.current.available).toBe(false);
    expect(result.current.scimManaged).toBe(true);
  });

  it("creates the role with no members for a directory-synced org", async () => {
    mocks.organization.scimEnabled = true;
    const result = setup();

    act(() => result.current.ensureRole());

    await waitFor(() => expect(mocks.createRole).toHaveBeenCalled());
    expect(
      mocks.createRole.mock.calls[0]![0].request.createRoleForm.memberIds,
    ).toBeUndefined();
  });

  it("reports the caller can read sessions when they hold chat:read", () => {
    mocks.hasScope.mockImplementation((scope: string) => scope === "chat:read");

    expect(setup().current.canReadSessions).toBe(true);
  });
});
