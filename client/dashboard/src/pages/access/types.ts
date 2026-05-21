import { Scope } from "@gram/client/models/components/rolegrant.js";
import type {
  Selector,
  Disposition,
  ResourceKind,
} from "@gram/client/models/components/selector.js";

export { Scope };
export type { Selector, Disposition, ResourceKind };

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
export type ResourceType = "org" | "project" | "mcp" | "environment";

/** The 4 MCP tool annotation hint keys. */
export type AnnotationHint =
  | "readOnlyHint"
  | "destructiveHint"
  | "idempotentHint"
  | "openWorldHint";

/** The tool-selection tabs in custom mode. */
export type CustomTab = "select" | "auto-groups";

/** Which panel the scope picker is displaying. Derived from selectors. */
export type ActivePanel =
  | "all"
  | "projects"
  | "servers"
  | "tools"
  | "collection";

/** Policy effect for a grant: allow (default) or deny. */
export type PolicyEffect = "allow" | "deny";

/** A single allow or deny rule within a scope grant. */
export interface ScopeRule {
  /** Unique identifier (React key + editing reference). */
  id: string;
  /** Whether this rule allows or denies access. */
  effect: PolicyEffect;
  /** null = unrestricted (all resources); Selector[] = constrained. */
  selectors: Selector[] | null;
  /** Annotation hints for annotation-level rules (UI-only). */
  annotations?: AnnotationHint[];
  /** Which custom tab was last active when editing (UI-only). */
  customTab?: CustomTab;
}

/** A scope within a role, containing one or more allow/deny rules. */
export interface RoleGrant {
  scope: Scope;
  /** The set of allow and deny rules for this scope. */
  rules: ScopeRule[];
}

/** Maps annotation hint keys to disposition values stored in selectors.
 * Must match the backend constants in authz/selector.go. */
export const ANNOTATION_TO_DISPOSITION: Record<AnnotationHint, Disposition> = {
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
