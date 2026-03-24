import type { ScopeDefinition, ScopeGroup } from "./types";

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
