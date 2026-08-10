// Local-only RBAC scope override state, persisted to localStorage. The SDK
// fetcher reads the same STORAGE_KEY via getRBACScopeOverrideHeader (see
// components/dev-toolbar-utils.ts) and turns it into the X-Gram-Scope-Override
// header on every request, so the key and shape here must stay in sync with it.

const RBAC_OVERRIDE_STORAGE_KEY = "gram-rbac-dev-override";

export type ResourceType =
  | "org"
  | "project"
  | "environment"
  | "skill"
  | "mcp"
  | "risk_policy"
  | "chat";

export const SCOPE_DEFS: {
  scope: string;
  label: string;
  resourceType: ResourceType;
  description: string;
}[] = [
  {
    scope: "org:read",
    label: "org:read",
    resourceType: "org",
    description: "View org metadata & members",
  },
  {
    scope: "org:admin",
    label: "org:admin",
    resourceType: "org",
    description: "Manage org settings & access",
  },
  {
    scope: "project:read",
    label: "project:read",
    resourceType: "project",
    description: "View projects & build resources",
  },
  {
    scope: "project:write",
    label: "project:write",
    resourceType: "project",
    description: "Modify projects & build resources",
  },
  {
    scope: "environment:read",
    label: "environment:read",
    resourceType: "environment",
    description: "View environments & their entries",
  },
  {
    scope: "environment:write",
    label: "environment:write",
    resourceType: "environment",
    description: "Create, edit, clone & delete environments",
  },
  {
    scope: "skill:read",
    label: "skill:read",
    resourceType: "skill",
    description: "View skills within projects",
  },
  {
    scope: "skill:write",
    label: "skill:write",
    resourceType: "skill",
    description: "Create and modify skills within projects",
  },
  {
    scope: "mcp:read",
    label: "mcp:read",
    resourceType: "mcp",
    description: "View MCP servers",
  },
  {
    scope: "mcp:write",
    label: "mcp:write",
    resourceType: "mcp",
    description: "Manage MCP servers",
  },
  {
    scope: "mcp:connect",
    label: "mcp:connect",
    resourceType: "mcp",
    description: "Execute MCP tool calls",
  },
  {
    scope: "risk_policy:evaluate",
    label: "risk_policy:evaluate",
    resourceType: "risk_policy",
    description: "Subject to targeted risk policies",
  },
  {
    scope: "risk_policy:bypass",
    label: "risk_policy:bypass",
    resourceType: "risk_policy",
    description: "Exempt from risk policy enforcement",
  },
  {
    scope: "risk_policy:block",
    label: "risk_policy:block",
    resourceType: "risk_policy",
    description: "Hard-block on risk policy violations",
  },
  {
    scope: "chat:read",
    label: "chat:read",
    resourceType: "chat",
    description: "Read all agent session transcripts",
  },
];

export const GROUP_ORDER: { key: ResourceType; label: string }[] = [
  { key: "org", label: "Organization" },
  { key: "project", label: "Project" },
  { key: "environment", label: "Environments" },
  { key: "skill", label: "Skills" },
  { key: "mcp", label: "MCP" },
  { key: "risk_policy", label: "Risk Policies" },
  { key: "chat", label: "Agent Sessions" },
];

export type ScopeState = {
  enabled: boolean;
  resources: string[] | null; // null = unrestricted, string[] = specific resource IDs
};

export type OverrideState = {
  enabled: boolean;
  scopes: Record<string, ScopeState>;
};

export function defaultScopeState(): Record<string, ScopeState> {
  return Object.fromEntries(
    SCOPE_DEFS.map((s) => [s.scope, { enabled: true, resources: null }]),
  );
}

// mergeWithDefaults overlays existing scope state on top of defaultScopeState
// so every scope in SCOPE_DEFS is present, but explicit user toggles win.
function mergeWithDefaults(
  existing: Record<string, ScopeState>,
): Record<string, ScopeState> {
  return { ...defaultScopeState(), ...existing };
}

export function loadState(): OverrideState {
  try {
    const raw = localStorage.getItem(RBAC_OVERRIDE_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      // Migrate from old boolean-only format
      if (
        parsed.scopes &&
        typeof Object.values(parsed.scopes)[0] !== "object"
      ) {
        return {
          enabled: parsed.enabled,
          scopes: mergeWithDefaults(
            Object.fromEntries(
              Object.entries(parsed.scopes).map(([scope, enabled]) => [
                scope,
                { enabled: enabled as boolean, resources: null },
              ]),
            ),
          ),
        };
      }
      // Merge SCOPE_DEFS defaults so any newly added scopes (since the user last
      // saved state) are materialized in localStorage. Without this, new entries
      // render as visually "enabled" via the per-row fallback in the JSX, but
      // getRBACScopeOverrideHeader only iterates keys that exist in state.scopes
      // — so the override header omits them despite them looking checked.
      return {
        enabled: parsed.enabled ?? false,
        scopes: mergeWithDefaults(parsed.scopes ?? {}),
      };
    }
  } catch {
    // ignore malformed localStorage
  }
  return { enabled: false, scopes: defaultScopeState() };
}

export function saveState(state: OverrideState): void {
  localStorage.setItem(RBAC_OVERRIDE_STORAGE_KEY, JSON.stringify(state));
}

const resourcesCacheKey = (orgId: string) => `gram-rbac-dev-resources:${orgId}`;

export type CachedResources = {
  projects: { id: string; label: string }[];
  mcps: { id: string; label: string }[];
};

export function loadCachedResources(orgId: string): CachedResources | null {
  try {
    const raw = localStorage.getItem(resourcesCacheKey(orgId));
    if (raw) return JSON.parse(raw);
  } catch {
    // ignore
  }
  return null;
}

export function saveCachedResources(
  resources: CachedResources,
  orgId: string,
): void {
  localStorage.setItem(resourcesCacheKey(orgId), JSON.stringify(resources));
}
