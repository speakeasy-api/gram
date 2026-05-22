/** Pure state helpers for the ChangeRoleDialog multi-role selector. */

export interface MinimalRole {
  id: string;
  name: string;
}

/**
 * Add a role to the selection. Returns the same array if already present.
 */
export function addRoleToSelection(
  selected: string[],
  roleId: string,
): string[] {
  if (selected.includes(roleId)) return selected;
  return [...selected, roleId];
}

/**
 * Remove a role from the selection. Enforces a minimum of 1 role —
 * returns the same array if removing would leave it empty.
 */
export function removeRoleFromSelection(
  selected: string[],
  roleId: string,
): string[] {
  if (selected.length <= 1) return selected;
  return selected.filter((id) => id !== roleId);
}

/**
 * Roles not yet in the selection, preserving the original order.
 */
export function getUnselectedRoles<R extends MinimalRole>(
  allRoles: R[],
  selectedIds: string[],
): R[] {
  const set = new Set(selectedIds);
  return allRoles.filter((r) => !set.has(r.id));
}

/**
 * True when the selected role set differs from the member's original roles.
 */
export function hasRolesChanged(
  selectedIds: string[],
  originalIds: string[],
): boolean {
  if (selectedIds.length !== originalIds.length) return true;
  const orig = new Set(originalIds);
  return selectedIds.some((id) => !orig.has(id));
}

/**
 * Determines whether the "Update Roles" button should be disabled.
 */
export function isUpdateDisabled(opts: {
  isPending: boolean;
  selectedIds: string[];
  originalIds: string[];
}): boolean {
  if (opts.isPending) return true;
  if (opts.selectedIds.length === 0) return true;
  return !hasRolesChanged(opts.selectedIds, opts.originalIds);
}

/**
 * Returns member IDs that currently hold a given role.
 * Used by CreateRoleDialog to pre-select members when editing.
 */
export function membersWithRole(
  members: { id: string; roleIds: string[] }[],
  roleId: string,
): string[] {
  return members.filter((m) => m.roleIds.includes(roleId)).map((m) => m.id);
}

/**
 * True when a member is locked to the role being edited — they're already
 * assigned and cannot be deselected from the member list.
 */
export function isMemberLockedToRole(
  isEditing: boolean,
  editingRoleId: string | undefined,
  memberRoleIds: string[],
): boolean {
  return isEditing && !!editingRoleId && memberRoleIds.includes(editingRoleId);
}

/**
 * Returns members that can be toggled on/off in the Create/Edit role dialog.
 * Members already assigned to the editing role are excluded (they're locked).
 */
export function getSelectableMembers<M extends { roleIds: string[] }>(
  members: M[],
  isEditing: boolean,
  editingRoleId: string | undefined,
): M[] {
  return members.filter(
    (m) => !isMemberLockedToRole(isEditing, editingRoleId, m.roleIds),
  );
}
