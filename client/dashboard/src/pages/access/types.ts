import { Scope } from "@gram/client/models/components/rolegrant.js";

export { Scope };

/** What kind of resource a scope protects. */
export type ResourceType = "org" | "project" | "mcp";

/** A multi-dimensional selector constraint on a grant. */
export type Selector = Record<string, string>;

/** The 4 MCP tool annotation hint keys. */
export type AnnotationHint =
  | "readOnlyHint"
  | "destructiveHint"
  | "idempotentHint"
  | "openWorldHint";

/** The three tool-selection tabs in custom mode. */
export type CustomTab = "select" | "auto-groups" | "collection";

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
export const ANNOTATION_TO_DISPOSITION: Record<AnnotationHint, string> = {
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
