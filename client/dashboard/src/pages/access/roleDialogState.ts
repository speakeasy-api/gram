import {
  isProjectFilteredResourceType,
  isProjectScopedResourceType,
  isProjectSelectableResourceType,
} from "./types";
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
  /** Agent principals assigned to the role */
  selectedAgents: Set<string>;
  /** Snapshot of form values when the dialog opened for editing */
  initial: {
    name: string;
    description: string;
    grantKeys: string;
    members: Set<string>;
    agents: Set<string>;
  };
}

export function visiblePermissionCount(
  grants: Array<{ scope?: string }>,
): number {
  return grants.filter(
    (grant) =>
      grant.scope !== undefined &&
      !grant.scope.startsWith("risk_policy:") &&
      !grant.scope.includes(":blocked_"),
  ).length;
}

/** Whether a selected set of principals differs from the initial snapshot */
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
    membersHaveChanged(input.selectedAgents, input.initial.agents) ||
    input.name !== input.initial.name ||
    input.description !== input.initial.description ||
    grantKeysString(input.grants) !== input.initial.grantKeys
  );
}

/** Whether the form fields are valid enough to submit.
 *  Description and grants are optional. */
function isFormValid(input: SaveButtonInput): boolean {
  return input.name.trim().length > 0;
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

function projectResourceNoun(resourceType: ResourceType): string | null {
  if (resourceType === "skill") return "skills";
  if (resourceType === "assistant") return "assistants";
  return null;
}

/** Short chip label for a rule (e.g. "All servers", "3 tools", "Project: foo"). */
export function computeRuleLabel(
  selectors: Selector[] | null,
  resourceType: ResourceType,
  projects: ProjectRef[],
): string {
  if (selectors === null) {
    if (isProjectFilteredResourceType(resourceType)) return "All assistants";
    return isProjectScopedResourceType(resourceType)
      ? "All projects"
      : "All servers";
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
    if (tools.length === 1) return tools[0]!.tool!;
    return `${tools.length} tools`;
  }

  const projectSels = selectors.filter((s) => s.projectId);
  if (projectSels.length > 0) {
    if (projectSels.length === 1) {
      const name = projects.find(
        (p) => p.id === projectSels[0]!.projectId!,
      )?.name;
      return name ? `Project: ${name}` : "1 project";
    }
    return `${projectSels.length} projects`;
  }

  // Project-selectable resource types (project, skill) store a
  // project id in resourceId, so the remaining selectors name projects rather
  // than servers.
  if (isProjectSelectableResourceType(resourceType)) {
    if (selectors.length === 1) {
      const name = projects.find(
        (p) => p.id === selectors[0]!.resourceId,
      )?.name;
      return name ? `Project: ${name}` : "1 project";
    }
    return `${selectors.length} projects`;
  }

  const noun = isProjectFilteredResourceType(resourceType)
    ? resourceType
    : "server";
  if (selectors.length === 1) return `1 ${noun}`;
  return `${selectors.length} ${noun}s`;
}

/** Plain-English tooltip describing what a rule does. */
export function computeRuleTooltip(
  effect: PolicyEffect,
  selectors: Selector[] | null,
  resourceType: ResourceType,
  projects: ProjectRef[],
): string {
  const verb = effect === "allow" ? "Permits" : "Excludes";

  if (selectors === null) {
    const resource = projectResourceNoun(resourceType);
    if (resource) {
      return `${verb} access to ${resource} in all projects in your org`;
    }
    if (resourceType === "project") {
      return `${verb} access to all projects in your org`;
    }
    return `${verb} access to all servers across your org`;
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
    if (tools.length === 1) return `${verb} access to ${tools[0]!.tool!}`;
    return `${verb} access to ${tools.length} tools`;
  }

  const projectSels = selectors.filter((s) => s.projectId);
  if (projectSels.length > 0) {
    if (projectSels.length === 1) {
      const name = projects.find(
        (p) => p.id === projectSels[0]!.projectId!,
      )?.name;
      const resources = isProjectFilteredResourceType(resourceType)
        ? `${resourceType}s`
        : "servers";
      return name
        ? `${verb} access to all ${resources} in ${name}`
        : `${verb} access to 1 project`;
    }
    return `${verb} access to ${projectSels.length} projects`;
  }

  if (isProjectSelectableResourceType(resourceType)) {
    if (selectors.length === 1) {
      const name = projects.find(
        (p) => p.id === selectors[0]!.resourceId,
      )?.name;
      const resource = projectResourceNoun(resourceType);
      if (!resource) {
        return name
          ? `${verb} access in ${name}`
          : `${verb} access to 1 project`;
      }
      return name
        ? `${verb} access to ${resource} in ${name}`
        : `${verb} access to ${resource} in 1 project`;
    }
    const resource = projectResourceNoun(resourceType);
    return resource
      ? `${verb} access to ${resource} in ${selectors.length} projects`
      : `${verb} access to ${selectors.length} projects`;
  }

  const noun = isProjectFilteredResourceType(resourceType)
    ? resourceType
    : "server";
  if (selectors.length === 1) return `${verb} access to 1 ${noun}`;
  return `${verb} access to ${selectors.length} ${noun}s`;
}
