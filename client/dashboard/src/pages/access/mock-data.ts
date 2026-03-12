import type { Member, Role, ScopeDefinition, ScopeGroup } from "./types";

// ---------------------------------------------------------------------------
// Scope catalogue — from the RFC's 7 system-defined scopes
// ---------------------------------------------------------------------------

export const SCOPES: ScopeDefinition[] = [
  {
    slug: "org:read",
    description: "View org settings, members, billing",
    resourceType: "org",
  },
  {
    slug: "org:admin",
    description: "Edit org, manage members, roles, domains, API keys",
    resourceType: "org",
  },
  {
    slug: "build:read",
    description:
      "View projects, deployments, toolsets, templates, environments",
    resourceType: "project",
  },
  {
    slug: "build:write",
    description: "Create/edit/delete/publish projects, deployments, toolsets",
    resourceType: "project",
  },
  {
    slug: "mcp:read",
    description: "View MCP server config and metadata",
    resourceType: "mcp",
  },
  {
    slug: "mcp:write",
    description: "Edit MCP server config and metadata",
    resourceType: "mcp",
  },
  {
    slug: "mcp:connect",
    description: "Connect to MCP servers and invoke tools",
    resourceType: "mcp",
  },
];

/** Scopes grouped by resource type for the UI */
export const SCOPE_GROUPS: ScopeGroup[] = [
  {
    label: "Organization",
    resourceType: "org",
    scopes: SCOPES.filter((s) => s.resourceType === "org"),
  },
  {
    label: "Build & Deploy",
    resourceType: "project",
    scopes: SCOPES.filter((s) => s.resourceType === "project"),
  },
  {
    label: "MCP Servers",
    resourceType: "mcp",
    scopes: SCOPES.filter((s) => s.resourceType === "mcp"),
  },
];

// ---------------------------------------------------------------------------
// Roles — matching the RFC's default roles + examples
// ---------------------------------------------------------------------------

const allScopes = SCOPES.map((s) => s.slug);

export const MOCK_ROLES: Role[] = [
  {
    id: "role_admin",
    name: "Admin",
    description: "Full access to all resources and settings",
    isSystem: true,
    grants: allScopes.map((scope) => ({ scope, resources: null })),
    memberCount: 2,
    createdAt: "2023-01-15T00:00:00Z",
    updatedAt: "2023-01-15T00:00:00Z",
  },
  {
    id: "role_member",
    name: "Member",
    description: "Read access plus MCP connectivity",
    isSystem: true,
    grants: [
      { scope: "org:read", resources: null },
      { scope: "build:read", resources: null },
      { scope: "mcp:read", resources: null },
      { scope: "mcp:connect", resources: null },
    ],
    memberCount: 5,
    createdAt: "2023-01-15T00:00:00Z",
    updatedAt: "2023-01-15T00:00:00Z",
  },
];

// ---------------------------------------------------------------------------
// Mock projects and MCP servers for resource pickers
// ---------------------------------------------------------------------------

export const MOCK_PROJECTS = [
  { id: "proj_1", name: "Marketing Website" },
  { id: "proj_2", name: "Customer Portal" },
  { id: "proj_3", name: "Admin Dashboard" },
  { id: "proj_4", name: "API Gateway" },
];

export const MOCK_MCP_SERVERS = [
  { id: "mcp_1", name: "payments" },
  { id: "mcp_2", name: "analytics" },
  { id: "mcp_3", name: "notifications" },
];

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

export const MOCK_MEMBERS: Member[] = [
  {
    id: "usr_1",
    name: "Sarah Chen",
    email: "sarah@company.com",
    roleId: "role_admin",
    joinedAt: "2023-01-15T00:00:00Z",
  },
  {
    id: "usr_2",
    name: "Alex Johnson",
    email: "alex@company.com",
    roleId: "role_admin",
    joinedAt: "2023-03-22T00:00:00Z",
  },
  {
    id: "usr_3",
    name: "Maya Patel",
    email: "maya@company.com",
    roleId: "role_member",
    joinedAt: "2023-05-10T00:00:00Z",
  },
  {
    id: "usr_4",
    name: "James Wilson",
    email: "james@company.com",
    roleId: "role_member",
    joinedAt: "2023-06-01T00:00:00Z",
  },
  {
    id: "usr_5",
    name: "Emma Davis",
    email: "emma@company.com",
    roleId: "role_member",
    joinedAt: "2023-07-18T00:00:00Z",
  },
  {
    id: "usr_6",
    name: "Michael Brown",
    email: "michael@company.com",
    roleId: "role_member",
    joinedAt: "2023-08-05T00:00:00Z",
  },
  {
    id: "usr_7",
    name: "Lisa Wang",
    email: "lisa@company.com",
    roleId: "role_member",
    joinedAt: "2023-09-12T00:00:00Z",
  },
];
