import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { SyncedAgentUser } from "@gram/client/models/components/syncedagentuser.js";
import { describe, expect, it } from "vitest";
import { countPluginInstalls } from "./plugin-reach";

function assignment(principalUrn: string): PluginAssignment {
  return { id: `a-${principalUrn}`, principalUrn } as PluginAssignment;
}

function synced(email: string): SyncedAgentUser {
  return { email } as SyncedAgentUser;
}

function member(
  id: string,
  email: string,
  roleIds: string[] = [],
): AccessMember {
  return {
    id,
    email,
    principalUrn: `user:${id}`,
    name: email,
    roleIds,
  } as AccessMember;
}

const engineering = {
  id: "role-eng",
  slug: "engineering",
  principalUrn: "role:organization:role-eng",
} as Role;

describe("countPluginInstalls", () => {
  it("returns 0 when no one is syncing", () => {
    expect(countPluginInstalls([assignment("*")], [], [], [])).toBe(0);
  });

  it("counts every synced identity for the org wildcard, member or not", () => {
    const syncedUsers = [synced("member@corp.com"), synced("stranger@x.com")];
    const members = [member("u1", "member@corp.com")];
    expect(
      countPluginInstalls([assignment("*")], syncedUsers, members, []),
    ).toBe(2);
  });

  it("counts user:all only for synced emails that resolve to a member", () => {
    const syncedUsers = [synced("member@corp.com"), synced("stranger@x.com")];
    const members = [member("u1", "member@corp.com")];
    expect(
      countPluginInstalls([assignment("user:all")], syncedUsers, members, []),
    ).toBe(1);
  });

  it("matches email assignments regardless of membership", () => {
    const syncedUsers = [synced("Stranger@X.com")];
    expect(
      countPluginInstalls(
        [assignment("email:stranger@x.com")],
        syncedUsers,
        [],
        [],
      ),
    ).toBe(1);
  });

  it("matches user:<id> assignments via the member's email", () => {
    const syncedUsers = [synced("member@corp.com")];
    const members = [member("u1", "member@corp.com")];
    expect(
      countPluginInstalls([assignment("user:u1")], syncedUsers, members, []),
    ).toBe(1);
  });

  it("matches role assignments by canonical role URN", () => {
    const syncedUsers = [synced("member@corp.com")];
    const members = [member("u1", "member@corp.com", ["role-eng"])];
    expect(
      countPluginInstalls(
        [assignment("role:organization:role-eng")],
        syncedUsers,
        members,
        [engineering],
      ),
    ).toBe(1);
  });

  it("matches role assignments stored in the legacy role:<slug> form", () => {
    const syncedUsers = [synced("member@corp.com")];
    const members = [member("u1", "member@corp.com", ["role-eng"])];
    expect(
      countPluginInstalls(
        [assignment("role:engineering")],
        syncedUsers,
        members,
        [engineering],
      ),
    ).toBe(1);
  });

  it("does not double-count a user matched by multiple assignments", () => {
    const syncedUsers = [synced("member@corp.com")];
    const members = [member("u1", "member@corp.com", ["role-eng"])];
    expect(
      countPluginInstalls(
        [assignment("user:u1"), assignment("role:engineering")],
        syncedUsers,
        members,
        [engineering],
      ),
    ).toBe(1);
  });
});
