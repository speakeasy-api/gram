import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { SyncedAgentUser } from "@gram/client/models/components/syncedagentuser.js";
import { WILDCARD_PRINCIPAL } from "./principals";

const EMAIL_PREFIX = "email:";
const ROLE_PREFIX = "role:";
const USER_PREFIX = "user:";
const ALL_USERS_PRINCIPAL = "user:all";

// countPluginInstalls estimates how many people actively running the device
// agent actually receive this plugin, by intersecting the org's synced-agent
// users with the plugin's assignment principals — the same coverage the agent's
// getPlugins applies when deciding what reaches a device:
//   *                  → every synced identity, member or not (org wildcard)
//   user:all           → every synced identity that resolves to an org member
//   email:<addr>       → the synced user with that email
//   user:<id>          → the member with that id
//   role:<kind>:<uuid> → every synced member holding that role
//
// Marketplace installs (Claude/Cursor/Codex) ship every published plugin and
// aren't attributed per plugin, so this count reflects device-agent reach only.
export function countPluginInstalls(
  assignments: PluginAssignment[],
  syncedUsers: SyncedAgentUser[],
  members: AccessMember[],
  roles: Role[],
): number {
  if (syncedUsers.length === 0) return 0;

  // Only the org wildcard reaches every synced identity regardless of
  // membership — a synced email that isn't an active member still receives
  // `*`-scoped plugins. `user:all` is member-scoped and handled in the loop.
  const coversWildcard = assignments.some(
    (a) => a.principalUrn === WILDCARD_PRINCIPAL,
  );
  if (coversWildcard) return syncedUsers.length;

  const coversAllMembers = assignments.some(
    (a) => a.principalUrn === ALL_USERS_PRINCIPAL,
  );

  const roleIdByUrn = new Map<string, string>();
  for (const role of roles) {
    if (role.principalUrn) roleIdByUrn.set(role.principalUrn, role.id);
  }

  const assignedEmails = new Set<string>();
  const assignedUserIds = new Set<string>();
  const assignedRoleIds = new Set<string>();
  for (const a of assignments) {
    const urn = a.principalUrn;
    if (urn.startsWith(EMAIL_PREFIX)) {
      assignedEmails.add(urn.slice(EMAIL_PREFIX.length).toLowerCase());
    } else if (urn.startsWith(USER_PREFIX)) {
      // user:all is not a concrete member id; it's handled by coversAllMembers.
      if (urn !== ALL_USERS_PRINCIPAL) {
        assignedUserIds.add(urn.slice(USER_PREFIX.length));
      }
    } else if (urn.startsWith(ROLE_PREFIX)) {
      const roleId = roleIdByUrn.get(urn);
      if (roleId) assignedRoleIds.add(roleId);
    }
  }

  const memberByEmail = new Map<string, AccessMember>();
  for (const m of members) memberByEmail.set(m.email.toLowerCase(), m);

  let covered = 0;
  for (const user of syncedUsers) {
    const email = user.email.toLowerCase();
    if (assignedEmails.has(email)) {
      covered++;
      continue;
    }
    // Non-members can only be reached by email or the wildcard (handled above),
    // never by user:all, user:<id>, or role: assignments.
    const member = memberByEmail.get(email);
    if (!member) continue;
    if (coversAllMembers) {
      covered++;
      continue;
    }
    if (assignedUserIds.has(member.id)) {
      covered++;
      continue;
    }
    if (member.roleIds.some((roleId) => assignedRoleIds.has(roleId))) {
      covered++;
    }
  }
  return covered;
}
