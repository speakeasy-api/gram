/** The system-defined scopes. Flat — no implied hierarchy. */
export type Scope =
  | "org:read"
  | "org:admin"
  | "build:read"
  | "build:write"
  | "mcp:read"
  | "mcp:write"
  | "mcp:connect"
  | "remote-mcp:read"
  | "remote-mcp:write"
  | "remote-mcp:connect";

/** What kind of resource a scope protects. */
export type ResourceType = "org" | "project" | "mcp" | "remote-mcp";

/** A single grant within a role: a scope + optional resource allowlist. */
export interface RoleGrant {
  scope: Scope;
  /** null = unrestricted (all resources); string[] = allowlist of resource IDs */
  resources: string[] | null;
}
