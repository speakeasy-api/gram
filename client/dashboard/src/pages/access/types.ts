import { Scope } from "@gram/client/models/components/rolegrant.js";
import type {
  Disposition as SelectorDisposition,
  Selector,
} from "@gram/client/models/components/selector.js";

/** Derive role slug from name the same way the server does (conv.ToSlug + "org-" prefix). */
export function toRoleSlug(name: string): string {
  let slug = name
    .replace(/_/g, " ")
    .replace(/[^a-zA-Z0-9\s-]/g, "")
    .toLowerCase()
    .replace(/[-\s]+/g, "-")
    .replace(/^-|-$/g, "");
  if (!slug.startsWith("org-")) {
    slug = "org-" + slug;
  }
  return slug;
}

/** What kind of resource a scope protects. */
export type ResourceType =
  | "org"
  | "project"
  | "mcp"
  | "environment"
  | "skill"
  | "risk_policy"
  | "chat"
  | "agent"
  | "logs";

export function isUnrestrictedResourceType(
  resourceType: ResourceType,
): resourceType is "org" | "environment" | "chat" | "agent" | "logs" {
  return (
    resourceType === "org" ||
    resourceType === "environment" ||
    resourceType === "chat" ||
    resourceType === "agent" ||
    resourceType === "logs"
  );
}

export function unrestrictedResourceLabel(resourceType: ResourceType): string {
  if (resourceType === "environment") return "All in project";
  if (resourceType === "chat") return "All sessions";
  if (resourceType === "agent") return "All agents";
  if (resourceType === "logs") return "All activity";
  return "All";
}

/**
 * The selector keys that narrow a logs permission to the people whose activity
 * the holder may read. Mirrors the actor_* keys in authz/selector.go.
 */
export const ACTOR_SELECTOR_LABELS = {
  actorDepartment: "department",
  actorGroup: "group",
  actorRole: "role",
} as const;

export type ActorSelectorKey = keyof typeof ACTOR_SELECTOR_LABELS;

export function actorSelectorConstraints(
  selector: Selector,
): { key: ActorSelectorKey; value: string }[] {
  const constraints: { key: ActorSelectorKey; value: string }[] = [];
  for (const key of Object.keys(ACTOR_SELECTOR_LABELS) as ActorSelectorKey[]) {
    const value = selector[key];
    if (value) constraints.push({ key, value });
  }
  return constraints;
}

/**
 * Describes how a logs permission is narrowed, for the read-only summary the
 * role editor shows. Narrowing is authored through the access API today, so the
 * editor has to be able to state a restriction it cannot itself change —
 * showing nothing would read as unrestricted access.
 */
export function actorScopeSummaries(selectors: Selector[] | null): string[] {
  if (!selectors) return [];
  const summaries: string[] = [];
  for (const selector of selectors) {
    const constraints = actorSelectorConstraints(selector);
    if (constraints.length === 0) continue;
    summaries.push(
      constraints
        .map(({ key, value }) => `${ACTOR_SELECTOR_LABELS[key]} ${value}`)
        .join(" and "),
    );
  }
  return summaries;
}

export function isProjectSelectableResourceType(
  resourceType: ResourceType,
): boolean {
  return resourceType === "project" || resourceType === "skill";
}

/** The 4 MCP tool annotation hint keys. */
export type AnnotationHint =
  | "readOnlyHint"
  | "destructiveHint"
  | "idempotentHint"
  | "openWorldHint";

/** The tool-selection tabs in custom mode. */
export type CustomTab = "select" | "auto-groups";

/** Which panel the scope picker is displaying. Derived from selectors. */
export type ActivePanel = "all" | "projects" | "servers" | "tools";

/**
 * UI-only rule effect. "deny" means an exception rule in the dashboard and is
 * serialized as an allow grant on the corresponding exclusion scope.
 */
export type PolicyEffect = "allow" | "deny";

/** A single allow or exception rule within a scope grant. */
export interface ScopeRule {
  /** Unique identifier (React key + editing reference). */
  id: string;
  /** Whether this UI rule allows access or defines an exception. */
  effect: PolicyEffect;
  /** null = unrestricted (all resources); Selector[] = constrained. */
  selectors: Selector[] | null;
  /** Annotation hints for annotation-level rules (UI-only). */
  annotations?: AnnotationHint[];
  /** Which custom tab was last active when editing (UI-only). */
  customTab?: CustomTab;
}

/** A scope within a role, containing one or more allow/exception rules. */
export interface RoleGrant {
  scope: Scope;
  /** The set of allow and exception rules for this scope. */
  rules: ScopeRule[];
}

/** Maps annotation hint keys to disposition values stored in selectors.
 * Must match the backend constants in authz/selector.go. */
export const ANNOTATION_TO_DISPOSITION: Record<
  AnnotationHint,
  SelectorDisposition
> = {
  readOnlyHint: "read_only",
  destructiveHint: "destructive",
  idempotentHint: "idempotent",
  openWorldHint: "open_world",
};

/** Reverse map: disposition value → annotation hint key. */
export const DISPOSITION_TO_ANNOTATION: Record<string, AnnotationHint> = {
  read_only: "readOnlyHint",
  destructive: "destructiveHint",
  idempotent: "idempotentHint",
  open_world: "openWorldHint",
};
