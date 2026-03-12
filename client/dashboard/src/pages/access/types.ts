// RBAC types — these will eventually be generated from the Goa design via Speakeasy SDK.
// For now they live here as local types used by the mock data and UI components.

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

export interface ScopeDefinition {
  slug: Scope;
  description: string;
  resourceType: ResourceType;
}

/** A single grant within a role: a scope + optional resource allowlist. */
export interface RoleGrant {
  scope: Scope;
  /** null = unrestricted (all resources); string[] = allowlist of resource IDs */
  resources: string[] | null;
}

export interface Role {
  id: string;
  name: string;
  description: string;
  isSystem: boolean;
  grants: RoleGrant[];
  memberCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface Member {
  id: string;
  name: string;
  email: string;
  photoUrl?: string;
  roleId: string;
  joinedAt: string;
}

export interface ScopeGroup {
  label: string;
  resourceType: ResourceType;
  scopes: ScopeDefinition[];
}
