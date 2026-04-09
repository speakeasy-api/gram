/** The 7 system-defined scopes. Flat — no implied hierarchy. */
export type Scope =
  | "org:read"
  | "org:admin"
  | "build:read"
  | "build:write"
  | "mcp:read"
  | "mcp:write"
  | "mcp:connect";

/** What kind of resource a scope protects. */
export type ResourceType = "org" | "project" | "mcp";

/** The 4 MCP tool annotation hint keys. */
export type AnnotationHint =
  | "readOnlyHint"
  | "destructiveHint"
  | "idempotentHint"
  | "openWorldHint";

/** A single grant within a role: a scope + optional resource allowlist. */
export interface RoleGrant {
  scope: Scope;
  /** null = unrestricted (all resources); string[] = allowlist of resource IDs */
  resources: string[] | null;
  /** Selected annotation hints for auto-group matching (MCP scopes only) */
  annotations?: AnnotationHint[];
}
