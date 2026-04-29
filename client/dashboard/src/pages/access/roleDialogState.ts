import type { RoleGrant } from "./types";

export interface SaveButtonInput {
  /** True when a create/update mutation is in flight */
  isMutating: boolean;
  /** True when dialog was opened to edit an existing role (vs create) */
  isEditing: boolean;
  /** True when the role being edited is a system role (Member/Admin) */
  isSystemRole: boolean;
  /** Current form values */
  name: string;
  description: string;
  grants: Record<string, RoleGrant>;
  selectedMembers: Set<string>;
  /** Snapshot of form values when the dialog opened for editing */
  initial: {
    name: string;
    description: string;
    grantKeys: string;
    members: Set<string>;
  };
}

/** Effective grant count — scopes with null (unrestricted) or non-empty selectors */
export function effectiveGrantCount(grants: Record<string, RoleGrant>): number {
  return Object.values(grants).filter(
    (g) => g.selectors === null || g.selectors.length > 0,
  ).length;
}

/** Whether the selected members differ from the initial snapshot */
export function membersHaveChanged(
  selected: Set<string>,
  initial: Set<string>,
): boolean {
  if (selected.size !== initial.size) return true;
  for (const id of selected) {
    if (!initial.has(id)) return true;
  }
  return false;
}

/** Sorted, comma-joined grant keys for cheap equality check */
export function grantKeysString(grants: Record<string, RoleGrant>): string {
  return Object.keys(grants).sort().join(",");
}

/** Whether any field has changed from the initial state */
export function hasFormChanges(input: SaveButtonInput): boolean {
  if (!input.isEditing) return true; // create mode — always "dirty"
  return (
    membersHaveChanged(input.selectedMembers, input.initial.members) ||
    input.name !== input.initial.name ||
    input.description !== input.initial.description ||
    grantKeysString(input.grants) !== input.initial.grantKeys
  );
}

/** Whether the form fields are valid enough to submit */
export function isFormValid(input: SaveButtonInput): boolean {
  if (input.isSystemRole) return true; // system roles only change members
  return (
    input.name.trim().length > 0 &&
    input.description.trim().length > 0 &&
    effectiveGrantCount(input.grants) > 0
  );
}

/** Whether non-member fields (name, description, grants) changed */
export function hasNonMemberChanges(input: SaveButtonInput): boolean {
  return (
    input.name !== input.initial.name ||
    input.description !== input.initial.description ||
    grantKeysString(input.grants) !== input.initial.grantKeys
  );
}

/** Returns true when the Save/Create button should be disabled */
export function isSaveDisabled(input: SaveButtonInput): boolean {
  if (input.isMutating) return true;
  // Create mode: full form validation always applies
  if (!input.isEditing) return !isFormValid(input);
  // Edit mode: just require something to have changed.
  // The role already exists and was valid — backend validates on submit.
  return !hasFormChanges(input);
}
