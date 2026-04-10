import { useOrganization } from "@/contexts/Auth";
import { Switch } from "./ui/switch";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronDown, ChevronUp, Shield } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

const STORAGE_KEY = "gram-rbac-dev-override";

type ResourceType = "org" | "project" | "mcp";

const SCOPE_DEFS: {
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
    scope: "build:read",
    label: "build:read",
    resourceType: "project",
    description: "View projects & build resources",
  },
  {
    scope: "build:write",
    label: "build:write",
    resourceType: "project",
    description: "Modify projects & build resources",
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
];

type ScopeState = {
  enabled: boolean;
  resources: string[] | null; // null = unrestricted, string[] = specific resource IDs
};

type OverrideState = {
  enabled: boolean;
  scopes: Record<string, ScopeState>;
};

function defaultScopeState(): Record<string, ScopeState> {
  return Object.fromEntries(
    SCOPE_DEFS.map((s) => [s.scope, { enabled: true, resources: null }]),
  );
}

function loadState(): OverrideState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      // Migrate from old boolean-only format
      if (
        parsed.scopes &&
        typeof Object.values(parsed.scopes)[0] !== "object"
      ) {
        return {
          enabled: parsed.enabled,
          scopes: Object.fromEntries(
            Object.entries(parsed.scopes).map(([scope, enabled]) => [
              scope,
              { enabled: enabled as boolean, resources: null },
            ]),
          ),
        };
      }
      return parsed;
    }
  } catch {
    // ignore malformed localStorage
  }
  return { enabled: false, scopes: defaultScopeState() };
}

function saveState(state: OverrideState) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
}

/**
 * Returns the X-Gram-Scope-Override header value if the dev override is active,
 * or null if disabled. Called by the SDK fetcher on every request.
 */
export function getRBACScopeOverrideHeader(): string | null {
  if (!import.meta.env.DEV) return null;
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const state: OverrideState = JSON.parse(raw);
    if (!state.enabled) return null;
    const parts = Object.entries(state.scopes)
      .filter(([, s]) => {
        if (typeof s === "boolean") return s; // backwards compat
        return s.enabled;
      })
      .map(([scope, s]) => {
        if (typeof s === "boolean") return scope;
        // Include resource IDs if restricted: build:read=proj_1|proj_2
        if (s.resources && s.resources.length > 0) {
          return `${scope}=${s.resources.join("|")}`;
        }
        return scope;
      });
    return parts.length > 0 ? parts.join(",") : null;
  } catch {
    return null;
  }
}

const GROUP_ORDER: { key: ResourceType; label: string }[] = [
  { key: "org", label: "Organization" },
  { key: "project", label: "Project" },
  { key: "mcp", label: "MCP" },
];

export function RBACDevToolbar() {
  const [state, setState] = useState<OverrideState>(loadState);
  const [collapsed, setCollapsed] = useState(true);
  const [expandedScope, setExpandedScope] = useState<string | null>(null);
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const projects = organization?.projects ?? [];

  useEffect(() => {
    saveState(state);
  }, [state]);

  const invalidate = useCallback(() => {
    setTimeout(() => queryClient.invalidateQueries(), 0);
  }, [queryClient]);

  const toggleEnabled = useCallback(() => {
    setState((prev) => ({ ...prev, enabled: !prev.enabled }));
    invalidate();
  }, [invalidate]);

  const toggleScope = useCallback(
    (scope: string) => {
      setState((prev) => ({
        ...prev,
        scopes: {
          ...prev.scopes,
          [scope]: {
            ...prev.scopes[scope],
            enabled: !prev.scopes[scope]?.enabled,
          },
        },
      }));
      if (state.enabled) invalidate();
    },
    [state.enabled, invalidate],
  );

  const setScopeResources = useCallback(
    (scope: string, resources: string[] | null) => {
      setState((prev) => ({
        ...prev,
        scopes: {
          ...prev.scopes,
          [scope]: { ...prev.scopes[scope], resources },
        },
      }));
      if (state.enabled) invalidate();
    },
    [state.enabled, invalidate],
  );

  const activeCount = Object.values(state.scopes).filter(
    (s) => s.enabled,
  ).length;

  return (
    <div className="fixed bottom-4 right-4 z-[9999] select-none">
      <div className={`
          rounded-xl border shadow-2xl backdrop-blur-md transition-all duration-200
          ${collapsed ? "w-auto" : "w-80"}
          ${state.enabled ? "bg-background/98 border-foreground/15 dark:bg-gray-950/98 dark:border-foreground/15" : "bg-white/98 border-border dark:bg-gray-950/98"}
        `}>
        {/* Header */}
        <button
          type="button"
          onClick={() => setCollapsed((c) => !c)}
          className={`
            flex items-center gap-2.5 w-full px-3.5 py-2.5 transition-colors
            ${collapsed ? "rounded-xl" : "rounded-t-xl"}
            hover:bg-black/[0.03] dark:hover:bg-white/[0.03]
          `}
        >
          <div className={`
              flex items-center justify-center w-6 h-6 rounded-md
              ${state.enabled ? "bg-foreground text-background" : "bg-muted text-muted-foreground"}
            `}>
            <Shield className="w-3.5 h-3.5" />
          </div>
          <span className="text-xs font-semibold tracking-wide text-foreground">
            RBAC Dev Tools
          </span>
          {state.enabled && (
            <span className="text-[10px] font-mono tabular-nums text-muted-foreground bg-muted px-1.5 py-0.5 rounded">
              {activeCount}/{SCOPE_DEFS.length}
            </span>
          )}
          <div className="ml-auto text-muted-foreground">
            {collapsed ? (
              <ChevronUp className="w-3.5 h-3.5" />
            ) : (
              <ChevronDown className="w-3.5 h-3.5" />
            )}
          </div>
        </button>

        {/* Panel */}
        {!collapsed && (
          <div className="border-t border-inherit">
            {/* Master toggle */}
            <div className="flex items-center justify-between px-3.5 py-2.5 border-b border-inherit">
              <div className="flex items-center gap-2">
                <div
                  className={`w-1.5 h-1.5 rounded-full ${state.enabled ? "bg-emerald-500 animate-pulse" : "bg-muted-foreground/30"}`}
                />
                <span className="text-xs font-medium text-foreground">
                  {state.enabled ? "Override active" : "Override disabled"}
                </span>
              </div>
              <Switch
                checked={state.enabled}
                onCheckedChange={toggleEnabled}
                aria-label="Toggle RBAC override"
              />
            </div>

            {/* Scope groups */}
            <div
              className={`px-2 py-2 space-y-1 max-h-[400px] overflow-y-auto ${!state.enabled ? "opacity-40 pointer-events-none" : ""}`}
            >
              {GROUP_ORDER.map((group) => {
                const scopes = SCOPE_DEFS.filter(
                  (s) => s.resourceType === group.key,
                );
                return (
                  <div key={group.key}>
                    <div className="px-2 pt-1.5 pb-1">
                      <span className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">
                        {group.label}
                      </span>
                    </div>
                    {scopes.map((def) => {
                      const scopeState = state.scopes[def.scope] ?? {
                        enabled: true,
                        resources: null,
                      };
                      const isExpanded = expandedScope === def.scope;
                      const isRestricted =
                        scopeState.resources !== null &&
                        scopeState.resources.length > 0;
                      const knownResources =
                        def.resourceType === "project" ||
                        def.resourceType === "mcp"
                          ? projects.map((p) => ({
                              id: p.id,
                              label: p.slug,
                            }))
                          : [];

                      return (
                        <div key={def.scope}>
                          <div className={`
                              flex items-center gap-2 px-2 py-1.5 rounded-lg cursor-pointer transition-colors
                              ${scopeState.enabled ? "hover:bg-black/[0.04] dark:hover:bg-white/[0.04]" : ""}
                            `} onClick={() => toggleScope(def.scope)}>
                            <div className={`
                                w-3.5 h-3.5 rounded border-[1.5px] flex items-center justify-center transition-all text-[9px]
                                ${scopeState.enabled ? "bg-foreground border-foreground text-background" : "border-muted-foreground/30 bg-transparent"}
                              `}>{scopeState.enabled && "✓"}</div>
                            <div className="flex-1 min-w-0">
                              <div className="flex items-center gap-1.5">
                                <span
                                  className={`text-xs font-mono font-medium ${
                                    scopeState.enabled
                                      ? "text-foreground"
                                      : "text-muted-foreground line-through"
                                  }`}
                                >
                                  {def.label}
                                </span>
                                {isRestricted && scopeState.enabled && (
                                  <span className="text-[9px] px-1 py-px rounded bg-blue-100 dark:bg-blue-900/50 text-blue-600 dark:text-blue-400 font-medium">
                                    scoped
                                  </span>
                                )}
                              </div>
                            </div>
                            {scopeState.enabled &&
                              def.resourceType !== "org" && (
                                <button
                                  type="button"
                                  className="p-0.5 rounded hover:bg-black/10 dark:hover:bg-white/10 text-muted-foreground"
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    setExpandedScope(
                                      isExpanded ? null : def.scope,
                                    );
                                  }}
                                >
                                  {isExpanded ? (
                                    <ChevronUp className="w-3 h-3" />
                                  ) : (
                                    <ChevronDown className="w-3 h-3" />
                                  )}
                                </button>
                              )}
                          </div>

                          {/* Resource picker */}
                          {/* TODO: verify resource-scoped overrides are enforced end-to-end (header → backend → UI) */}
                          {isExpanded && scopeState.enabled && (
                            <ResourcePicker
                              knownResources={knownResources}
                              selected={scopeState.resources}
                              onChange={(resources) =>
                                setScopeResources(def.scope, resources)
                              }
                            />
                          )}
                        </div>
                      );
                    })}
                  </div>
                );
              })}
            </div>

            {/* Footer */}
            <div className="border-t border-inherit px-3.5 py-2 flex items-center justify-between">
              <span className="text-[10px] text-muted-foreground">
                local only
              </span>
              <button
                type="button"
                className="text-[10px] text-muted-foreground hover:text-foreground transition-colors"
                onClick={() => {
                  setState({
                    enabled: false,
                    scopes: defaultScopeState(),
                  });
                  invalidate();
                }}
              >
                Reset
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function ResourcePicker({
  knownResources,
  selected,
  onChange,
}: {
  knownResources: { id: string; label: string }[];
  selected: string[] | null;
  onChange: (resources: string[] | null) => void;
}) {
  const [query, setQuery] = useState("");
  const isAll = selected === null;

  const alreadySelected = new Set(selected ?? []);

  // Filter known resources by query
  const suggestions = knownResources.filter((r) => {
    if (!query) return true;
    const q = query.toLowerCase();
    return r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q);
  });

  const toggleResource = (id: string) => {
    if (alreadySelected.has(id)) {
      removeResource(id);
    } else {
      onChange([...(selected ?? []), id]);
    }
    setQuery("");
  };

  const removeResource = (id: string) => {
    const next = (selected ?? []).filter((s) => s !== id);
    onChange(next.length === 0 ? null : next);
  };

  return (
    <div className="ml-7 mr-2 mb-1.5 rounded-lg border border-dashed border-border bg-muted/30 p-2 space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-[10px] font-medium text-muted-foreground">
          Resources
        </span>
        <button
          type="button"
          className={`text-[10px] transition-colors ${isAll ? "text-foreground font-medium" : "text-muted-foreground hover:text-foreground"}`}
          onClick={() => {
            onChange(null);
            setQuery("");
          }}
        >
          All (wildcard)
        </button>
      </div>

      {/* Filter input (only shown when there are many resources) */}
      {knownResources.length > 5 && (
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="filter…"
          className="w-full text-[11px] font-mono bg-background border border-border rounded px-1.5 py-0.5 text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-foreground/30"
        />
      )}

      {/* Resource list */}
      <div className="max-h-[140px] overflow-y-auto space-y-0.5">
        {suggestions.map((r) => {
          const isChecked = alreadySelected.has(r.id);
          return (
            <button
              key={r.id}
              type="button"
              className={`w-full text-left px-1.5 py-0.5 rounded hover:bg-muted/50 transition-colors flex items-center gap-1.5 ${isChecked ? "bg-muted/30" : ""}`}
              onClick={() => toggleResource(r.id)}
            >
              <div
                className={`w-3 h-3 shrink-0 rounded-sm border flex items-center justify-center text-[8px] transition-all ${isChecked ? "bg-foreground border-foreground text-background" : "border-muted-foreground/30"}`}
              >
                {isChecked && "✓"}
              </div>
              <span className="text-[11px] font-mono text-foreground truncate">
                {r.label}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
