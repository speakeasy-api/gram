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
export type ResourceType = "org" | "project" | "mcp";

/** The 4 MCP tool annotation hint keys. */
export type AnnotationHint =
  | "readOnlyHint"
  | "destructiveHint"
  | "idempotentHint"
  | "openWorldHint";

/** The tool-selection tabs in custom mode. */
export type CustomTab = "select" | "auto-groups";

/** Which panel the scope picker is displaying. Derived from selectors. */
export type ActivePanel = "all" | "servers" | "tools" | "collection";

/** A single grant within a role: a scope + optional selector constraints. */
export interface RoleGrant {
  scope: Scope;
  /** null = unrestricted; Selector[] = constrained by selectors */
  selectors: Selector[] | null;
  /** Selected annotation hints for auto-group matching (MCP scopes only) */
  annotations?: AnnotationHint[];
  /** Which custom tab was last active (UI-only, not persisted to backend) */
  customTab?: CustomTab;
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
