import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { SyncedAgentUser } from "@gram/client/models/components/syncedagentuser.js";
import { WILDCARD_PRINCIPAL } from "./principals";

const EMAIL_PREFIX = "email:";
const ROLE_PREFIX = "role:";
const USER_PREFIX = "user:";

// countPluginInstalls estimates how many people actively running the device
// agent actually receive this plugin, by intersecting the org's synced-agent
// users with the plugin's assignment principals — the same coverage the agent's
// getPlugins applies when deciding what reaches a device:
//   *  / user:all      → everyone syncing is covered
//   email:<addr>       → the synced user with that email
//   user:<id>          → the member with that id
//   role:<kind>:<id>   → every synced member holding that role
//
// Marketplace installs (Claude/Cursor/Codex) ship every published plugin and
// aren't attributed per plugin, so this count reflects device-agent reach only.
export function countPluginInstalls(
  assignments: PluginAssignment[],
  syncedUsers: SyncedAgentUser[],
  members: AccessMember[],
  roleByUrn: Map<string, Role>,
): number {
  if (syncedUsers.length === 0) return 0;

  // Everyone-style assignment: every synced user receives it, no further
  // resolution needed.
  const coversEveryone = assignments.some(
    (a) =>
      a.principalUrn === WILDCARD_PRINCIPAL || a.principalUrn === "user:all",
  );
  if (coversEveryone) return syncedUsers.length;

  const assignedEmails = new Set<string>();
  const assignedUserIds = new Set<string>();
  const assignedRoleIds = new Set<string>();
  for (const a of assignments) {
    const urn = a.principalUrn;
    if (urn.startsWith(EMAIL_PREFIX)) {
      assignedEmails.add(urn.slice(EMAIL_PREFIX.length).toLowerCase());
    } else if (urn.startsWith(USER_PREFIX)) {
      assignedUserIds.add(urn.slice(USER_PREFIX.length));
    } else if (urn.startsWith(ROLE_PREFIX)) {
      const roleId = roleByUrn.get(urn)?.id;
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
    const member = memberByEmail.get(email);
    if (!member) continue;
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
