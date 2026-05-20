import type { PolicyEffect, ResourceType, RoleGrant } from "./types";
import type { Selector } from "@gram/client/models/components/selector.js";

type ProjectRef = { id: string; name: string };

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

/** Effective grant count — scopes with at least one allow rule that has content. */
export function effectiveGrantCount(grants: Record<string, RoleGrant>): number {
  return Object.values(grants).filter((g) =>
    g.rules.some(
      (r) =>
        r.effect === "allow" &&
        (r.selectors === null || r.selectors.length > 0),
    ),
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

/** Sorted, comma-joined grant keys for cheap equality check.
 *  Encodes each rule's effect and selector count so any change marks dirty. */
export function grantKeysString(grants: Record<string, RoleGrant>): string {
  return Object.entries(grants)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, g]) => {
      const summary = g.rules
        .map((r) => {
          const selKey =
            r.selectors === null ? "*" : String(r.selectors.length);
          return `${r.effect}:${selKey}`;
        })
        .sort()
        .join("+");
      return `${key}[${summary}]`;
    })
    .join(",");
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

// ─── Rule label helpers ─────────────────────────────────────────────────────

const DISPOSITION_LABELS: Record<string, string> = {
  read_only: "Read-only",
  destructive: "Destructive",
  idempotent: "Idempotent",
  open_world: "Open-world",
};

const DISPOSITION_LABELS_LOWER: Record<string, string> = {
  read_only: "read-only",
  destructive: "destructive",
  idempotent: "idempotent",
  open_world: "open-world",
};

/** Short chip label for a rule (e.g. "All servers", "3 tools", "Project: foo"). */
export function computeRuleLabel(
  selectors: Selector[] | null,
  resourceType: ResourceType,
  projects: ProjectRef[],
): string {
  if (selectors === null) {
    return resourceType === "project" ? "All projects" : "All servers";
  }
  if (selectors.length === 0) return "Select\u2026";

  const dispositions = selectors.filter((s) => s.disposition);
  if (dispositions.length > 0) {
    const labels = dispositions.map(
      (s) => DISPOSITION_LABELS[s.disposition!] ?? s.disposition,
    );
    if (labels.length === 1) return `${labels[0]} tools`;
    return labels.join(", ");
  }

  const tools = selectors.filter((s) => s.tool);
  if (tools.length > 0) {
    if (tools.length === 1) return tools[0].tool!;
    return `${tools.length} tools`;
  }

  const projectSels = selectors.filter((s) => s.projectId);
  if (projectSels.length > 0) {
    if (projectSels.length === 1) {
      const name = projects.find(
        (p) => p.id === projectSels[0].projectId,
      )?.name;
      return name ? `Project: ${name}` : "1 project";
    }
    return `${projectSels.length} projects`;
  }

  if (selectors.length === 1) return "1 server";
  return `${selectors.length} servers`;
}

/** Plain-English tooltip describing what a rule does. */
export function computeRuleTooltip(
  effect: PolicyEffect,
  selectors: Selector[] | null,
  resourceType: ResourceType,
  projects: ProjectRef[],
): string {
  const verb = effect === "allow" ? "Permits" : "Denies";

  if (selectors === null) {
    return resourceType === "project"
      ? `${verb} access to all projects in your org`
      : `${verb} access to all servers across your org`;
  }
  if (selectors.length === 0) return `${verb} access (none selected)`;

  const dispositions = selectors.filter((s) => s.disposition);
  if (dispositions.length > 0) {
    const labels = dispositions.map(
      (s) => DISPOSITION_LABELS_LOWER[s.disposition!] ?? s.disposition,
    );
    return `${verb} access to all ${labels.join(" and ")} tools`;
  }

  const tools = selectors.filter((s) => s.tool);
  if (tools.length > 0) {
    if (tools.length === 1) return `${verb} access to ${tools[0].tool}`;
    return `${verb} access to ${tools.length} tools`;
  }

  const projectSels = selectors.filter((s) => s.projectId);
  if (projectSels.length > 0) {
    if (projectSels.length === 1) {
      const name = projects.find(
        (p) => p.id === projectSels[0].projectId,
      )?.name;
      return name
        ? `${verb} access to all servers in ${name}`
        : `${verb} access to 1 project`;
    }
    return `${verb} access to ${projectSels.length} projects`;
  }

  if (selectors.length === 1) return `${verb} access to 1 server`;
  return `${verb} access to ${selectors.length} servers`;
}
