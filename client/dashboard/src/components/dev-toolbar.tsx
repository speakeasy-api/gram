import { useIsAdmin, useOrganization, useSession } from "@/contexts/Auth";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { Switch } from "./ui/switch";
import { useQueryClient } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronUp,
  Crown,
  GripVertical,
  Shield,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

const STORAGE_KEY = "gram-rbac-dev-override";
const HIDDEN_KEY = "gram-dev-toolbar-hidden";
const SUPER_ADMIN_KEY = "gram-dev-super-admin";

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

const resourcesCacheKey = (orgId: string) => `gram-rbac-dev-resources:${orgId}`;

type CachedResources = {
  projects: { id: string; label: string }[];
  mcps: { id: string; label: string }[];
};

function loadCachedResources(orgId: string): CachedResources | null {
  try {
    const raw = localStorage.getItem(resourcesCacheKey(orgId));
    if (raw) return JSON.parse(raw);
  } catch {
    // ignore
  }
  return null;
}

function saveCachedResources(resources: CachedResources, orgId: string) {
  localStorage.setItem(resourcesCacheKey(orgId), JSON.stringify(resources));
}

const POSITION_KEY = "gram-rbac-dev-toolbar-pos";

function loadPosition(): { x: number; y: number } | null {
  try {
    const raw = localStorage.getItem(POSITION_KEY);
    if (raw) {
      const pos = JSON.parse(raw);
      return {
        x: Math.max(0, Math.min(pos.x, window.innerWidth - 320)),
        y: Math.max(0, Math.min(pos.y, window.innerHeight - 44)),
      };
    }
  } catch {
    // ignore
  }
  return null;
}

const GROUP_ORDER: { key: ResourceType; label: string }[] = [
  { key: "org", label: "Organization" },
  { key: "project", label: "Project" },
  { key: "mcp", label: "MCP" },
];

export function RBACDevToolbar() {
  const { session } = useSession();
  const isAdmin = useIsAdmin();
  const [hidden, setHidden] = useState(
    () => localStorage.getItem(HIDDEN_KEY) === "1",
  );

  // Ctrl+Shift+D toggles toolbar visibility back
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey && e.shiftKey && e.key === "D") {
        e.preventDefault();
        setHidden((prev) => {
          const next = !prev;
          if (next) {
            localStorage.setItem(HIDDEN_KEY, "1");
          } else {
            localStorage.removeItem(HIDDEN_KEY);
          }
          return next;
        });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Don't render when unauthenticated (e.g. login page) to avoid firing
  // API calls like toolsets.listForOrg that will 401 and trigger the error boundary.
  if (!session) return null;
  // Always visible in dev; in other environments, restricted to superadmins.
  if (!(import.meta.env.DEV || isAdmin)) return null;
  if (hidden) return null;

  return (
    <RBACDevToolbarInner
      onHide={() => {
        localStorage.setItem(HIDDEN_KEY, "1");
        setHidden(true);
      }}
    />
  );
}

function RBACDevToolbarInner({ onHide }: { onHide: () => void }) {
  const [state, setState] = useState<OverrideState>(loadState);
  const [collapsed, setCollapsed] = useState(true);
  const [activeTab, setActiveTab] = useState("rbac");
  const [pos, setPos] = useState<{ x: number; y: number } | null>(loadPosition);
  const [superAdmin, setSuperAdmin] = useState(
    () => localStorage.getItem(SUPER_ADMIN_KEY) === "1",
  );
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const liveProjects = (organization?.projects ?? []).map((project) => ({
    id: project.id,
    label: project.slug,
  }));
  const { data: toolsetsData } = useListToolsetsForOrg(undefined, undefined, {
    throwOnError: false,
  });
  const liveMcps = (toolsetsData?.toolsets ?? []).map((toolset) => ({
    id: toolset.id,
    label: toolset.name,
  }));

  // Cache the full resource list when overrides are off so the toolbar
  // still shows all projects/MCPs after the user restricts scopes.
  const orgProjects = organization?.projects;
  const toolsets = toolsetsData?.toolsets;
  const orgId = organization?.id ?? "";
  useEffect(() => {
    if (
      !state.enabled &&
      orgId &&
      orgProjects &&
      orgProjects.length > 0 &&
      toolsets
    ) {
      saveCachedResources(
        {
          projects: orgProjects.map((p) => ({ id: p.id, label: p.slug })),
          mcps: toolsets.map((t) => ({ id: t.id, label: t.name })),
        },
        orgId,
      );
    }
  }, [state.enabled, orgId, orgProjects, toolsets]);

  const cached = orgId ? loadCachedResources(orgId) : null;
  const projectResources =
    state.enabled && cached ? cached.projects : liveProjects;
  const mcpResources = state.enabled && cached ? cached.mcps : liveMcps;
  const rootRef = useRef<HTMLDivElement>(null);
  const dragOffset = useRef<{
    ox: number;
    oy: number;
    startX: number;
    startY: number;
  } | null>(null);
  const hasDragged = useRef(false);

  useEffect(() => {
    saveState(state);
  }, [state]);

  useEffect(() => {
    if (pos) localStorage.setItem(POSITION_KEY, JSON.stringify(pos));
  }, [pos]);

  // Clamp position so the expanded toolbar stays within the viewport
  useLayoutEffect(() => {
    if (collapsed) return;
    const el = rootRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    if (rect.bottom > window.innerHeight) {
      const clampedY = Math.max(0, window.innerHeight - rect.height - 8);
      setPos({ x: rect.left, y: clampedY });
    }
  }, [collapsed]);

  const onPointerDown = useCallback(
    (e: React.PointerEvent<HTMLButtonElement>) => {
      if (e.button !== 0) return;
      const el = rootRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      dragOffset.current = {
        ox: e.clientX - rect.left,
        oy: e.clientY - rect.top,
        startX: e.clientX,
        startY: e.clientY,
      };
      hasDragged.current = false;
    },
    [],
  );

  // Window-level listeners so drag works even when cursor moves fast off the button
  useEffect(() => {
    const onMove = (e: PointerEvent) => {
      if (!dragOffset.current) return;
      const { ox, oy, startX, startY } = dragOffset.current;
      if (!hasDragged.current) {
        if (Math.hypot(e.clientX - startX, e.clientY - startY) < 4) return;
        hasDragged.current = true;
      }
      const el = rootRef.current;
      const w = el ? el.offsetWidth : 320;
      const h = el ? el.offsetHeight : 50;
      const newX = Math.max(0, Math.min(window.innerWidth - w, e.clientX - ox));
      const newY = Math.max(
        0,
        Math.min(window.innerHeight - h, e.clientY - oy),
      );
      setPos({ x: newX, y: newY });
    };
    const onUp = () => {
      dragOffset.current = null;
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
  }, []);

  // Collapse when clicking outside
  useEffect(() => {
    if (collapsed) return;
    const onDown = (e: MouseEvent) => {
      if (rootRef.current?.contains(e.target as Node)) return;
      setCollapsed(true);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [collapsed]);

  const invalidate = useCallback(() => {
    setTimeout(() => {
      queryClient.invalidateQueries();
      window.dispatchEvent(new Event("rbac-override-change"));
    }, 0);
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

  return createPortal(
    <div
      ref={rootRef}
      className="pointer-events-auto fixed z-[99999] select-none"
      style={pos ? { left: pos.x, top: pos.y } : { left: 16, bottom: 16 }}
    >
      <div className={`
          w-80 rounded-xl border shadow-2xl backdrop-blur-md transition-all
          duration-200
          ${state.enabled ? "bg-background/98 border-foreground/15 dark:border-foreground/15 dark:bg-gray-950/98" : "border-border bg-white/98 dark:bg-gray-950/98"}
        `}>
        {/* Header row */}
        <div className={`
            flex w-full items-center gap-2.5 px-3.5 py-2.5
            ${collapsed ? "rounded-xl" : "rounded-t-xl"}
          `}>
          <button
            type="button"
            onPointerDown={onPointerDown}
            onClick={() => {
              if (hasDragged.current) {
                hasDragged.current = false;
                return;
              }
              setCollapsed((c) => !c);
            }}
            className="flex flex-1 cursor-grab items-center gap-2.5 active:cursor-grabbing"
          >
            <GripVertical className="text-muted-foreground/40 h-3.5 w-3.5 shrink-0" />
            <span className="rounded bg-amber-100 px-1.5 py-0.5 font-mono text-[10px] font-semibold tracking-widest text-amber-700 dark:bg-amber-900/40 dark:text-amber-400">
              DEV
            </span>
            <span className="text-muted-foreground text-xs font-semibold">
              Developer Toolkit
            </span>
          </button>
          <div className="ml-auto flex items-center gap-0.5">
            <button
              type="button"
              onClick={() => {
                if (hasDragged.current) {
                  hasDragged.current = false;
                  return;
                }
                setCollapsed((c) => !c);
              }}
              className="text-muted-foreground hover:bg-accent hover:text-foreground rounded-full p-1 transition-colors"
            >
              {collapsed ? (
                <ChevronUp className="h-3.5 w-3.5" />
              ) : (
                <ChevronDown className="h-3.5 w-3.5" />
              )}
            </button>
            <button
              type="button"
              title="Hide toolbar (Ctrl+Shift+D to restore)"
              onClick={onHide}
              className="text-muted-foreground hover:bg-accent hover:text-foreground rounded-full p-1 transition-colors"
            >
              <svg
                width="10"
                height="10"
                viewBox="0 0 10 10"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
              >
                <path d="M2 2l6 6M8 2l-6 6" />
              </svg>
            </button>
          </div>
        </div>

        {/* Panel */}
        {!collapsed && (
          <div className="border-t border-inherit">
            {/* Tab bar */}
            <div className="flex border-b border-inherit px-2">
              <button
                type="button"
                onClick={() => setActiveTab("rbac")}
                className={`-mb-px flex items-center gap-1.5 border-b-2 px-2 py-2 text-[11px] font-medium transition-colors ${
                  activeTab === "rbac"
                    ? "border-foreground text-foreground"
                    : "text-muted-foreground hover:text-foreground border-transparent"
                }`}
              >
                <Shield className="h-3 w-3" />
                RBAC
                {state.enabled && (
                  <span className="bg-muted text-muted-foreground rounded px-1 py-px font-mono text-[9px] tabular-nums">
                    {activeCount}/{SCOPE_DEFS.length}
                  </span>
                )}
              </button>
              {import.meta.env.DEV && (
                <button
                  type="button"
                  onClick={() => setActiveTab("superadmin")}
                  className={`-mb-px flex items-center gap-1.5 border-b-2 px-2 py-2 text-[11px] font-medium transition-colors ${
                    activeTab === "superadmin"
                      ? "border-foreground text-foreground"
                      : "text-muted-foreground hover:text-foreground border-transparent"
                  }`}
                >
                  <Crown className="h-3 w-3" />
                  Super Admin
                </button>
              )}
            </div>

            {/* RBAC tab */}
            {activeTab === "superadmin" && (
              <div className="flex items-center justify-between px-3.5 py-3">
                <div className="flex items-center gap-2">
                  <div
                    className={`h-1.5 w-1.5 rounded-full ${superAdmin ? "animate-pulse bg-amber-500" : "bg-muted-foreground/30"}`}
                  />
                  <span className="text-foreground text-xs font-medium">
                    {superAdmin ? "Super admin active" : "Super admin off"}
                  </span>
                </div>
                <Switch
                  checked={superAdmin}
                  onCheckedChange={(checked) => {
                    setSuperAdmin(checked);
                    localStorage.setItem(SUPER_ADMIN_KEY, checked ? "1" : "0");
                    invalidate();
                  }}
                  aria-label="Toggle super admin"
                />
              </div>
            )}

            {activeTab === "rbac" && (
              <>
                {/* Master toggle */}
                <div className="flex items-center justify-between border-b border-inherit px-3.5 py-2.5">
                  <div className="flex items-center gap-2">
                    <div
                      className={`h-1.5 w-1.5 rounded-full ${state.enabled ? "animate-pulse bg-emerald-500" : "bg-muted-foreground/30"}`}
                    />
                    <span className="text-foreground text-xs font-medium">
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
                  className={`max-h-[400px] space-y-1 overflow-y-auto px-2 py-2 ${!state.enabled ? "pointer-events-none opacity-40" : ""}`}
                >
                  {GROUP_ORDER.map((group) => {
                    const scopes = SCOPE_DEFS.filter(
                      (s) => s.resourceType === group.key,
                    );
                    return (
                      <div key={group.key}>
                        <div className="px-2 pt-1.5 pb-1">
                          <span className="text-muted-foreground text-[10px] font-semibold tracking-widest uppercase">
                            {group.label}
                          </span>
                        </div>
                        {scopes.map((def) => {
                          const scopeState = state.scopes[def.scope] ?? {
                            enabled: true,
                            resources: null,
                          };
                          const isRestricted =
                            scopeState.resources !== null &&
                            scopeState.resources.length > 0;
                          let knownResources: { id: string; label: string }[] =
                            [];
                          if (def.resourceType === "project") {
                            knownResources = projectResources;
                          } else if (def.resourceType === "mcp") {
                            knownResources = mcpResources;
                          }

                          return (
                            <div key={def.scope}>
                              <div className={`
                              flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 transition-colors
                              ${scopeState.enabled ? "hover:bg-black/[0.04] dark:hover:bg-white/[0.04]" : ""}
                            `} onClick={() => toggleScope(def.scope)}>
                                <div className={`
                                flex h-3.5 w-3.5 items-center justify-center rounded border-[1.5px] text-[9px] transition-all
                                ${scopeState.enabled ? "bg-foreground border-foreground text-background" : "border-muted-foreground/30 bg-transparent"}
                              `}>{scopeState.enabled && "✓"}</div>
                                <div className="min-w-0 flex-1">
                                  <div className="flex items-center gap-1.5">
                                    <span
                                      className={`font-mono text-xs font-medium ${
                                        scopeState.enabled
                                          ? "text-foreground"
                                          : "text-muted-foreground line-through"
                                      }`}
                                    >
                                      {def.label}
                                    </span>
                                    {isRestricted && scopeState.enabled && (
                                      <span className="rounded bg-blue-100 px-1 py-px text-[9px] font-medium text-blue-600 dark:bg-blue-900/50 dark:text-blue-400">
                                        scoped
                                      </span>
                                    )}
                                  </div>
                                </div>
                              </div>

                              {scopeState.enabled &&
                                knownResources.length > 0 && (
                                  <ResourceDropdown
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
                <div className="flex items-center justify-between border-t border-inherit px-3.5 py-2">
                  <span className="text-muted-foreground text-[10px]">
                    local only
                  </span>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground text-[10px] transition-colors"
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
              </>
            )}
          </div>
        )}
      </div>
    </div>,
    document.body,
  );
}

function ResourceDropdown({
  knownResources,
  selected,
  onChange,
}: {
  knownResources: { id: string; label: string }[];
  selected: string[] | null;
  onChange: (resources: string[] | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const isAll = selected === null;
  const selectedSet = new Set(selected ?? []);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        !triggerRef.current?.contains(target) &&
        !panelRef.current?.contains(target)
      )
        setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  const filtered = knownResources.filter((r) => {
    if (!query) return true;
    const q = query.toLowerCase();
    return r.label.toLowerCase().includes(q) || r.id.toLowerCase().includes(q);
  });

  const toggleResource = (id: string) => {
    if (selectedSet.has(id)) {
      const next = (selected ?? []).filter((s) => s !== id);
      onChange(next.length === 0 ? null : next);
    } else {
      onChange([...(selected ?? []), id]);
    }
  };

  const label = isAll
    ? "All resources"
    : `${selectedSet.size} of ${knownResources.length} selected`;

  // Compute position from trigger rect so the portal panel aligns correctly
  const triggerRect = triggerRef.current?.getBoundingClientRect();

  return (
    <div className="mr-2 mb-1.5 ml-7">
      <button
        ref={triggerRef}
        type="button"
        className="border-border bg-background hover:bg-muted/50 flex w-full items-center justify-between rounded-md border px-2 py-1 text-left"
        onClick={(e) => {
          e.stopPropagation();
          setOpen(!open);
        }}
      >
        <span className="text-muted-foreground truncate text-[11px]">
          {label}
        </span>
        <ChevronDown
          className={`text-muted-foreground h-3 w-3 shrink-0 transition-transform ${open ? "rotate-180" : ""}`}
        />
      </button>

      {open &&
        triggerRect &&
        createPortal(
          <div
            ref={panelRef}
            className="border-border bg-background fixed z-[999999] rounded-md border shadow-lg"
            style={{
              top: triggerRect.bottom + 4,
              left: triggerRect.left,
              width: triggerRect.width,
            }}
          >
            {/* All wildcard option */}
            <button
              type="button"
              className="hover:bg-muted/50 flex w-full items-center gap-1.5 border-b border-inherit px-2 py-1.5 text-left transition-colors"
              onClick={() => {
                onChange(null);
                setQuery("");
              }}
            >
              <div
                className={`flex h-3 w-3 shrink-0 items-center justify-center rounded-sm border text-[8px] transition-all ${isAll ? "bg-foreground border-foreground text-background" : "border-muted-foreground/30"}`}
              >
                {isAll && "✓"}
              </div>
              <span className="text-foreground text-[11px] font-medium">
                All (wildcard)
              </span>
            </button>

            {/* Filter input */}
            {knownResources.length > 5 && (
              <div className="border-b border-inherit px-2 py-1">
                <input
                  type="text"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="Filter…"
                  className="text-foreground placeholder:text-muted-foreground/50 w-full bg-transparent text-[11px] outline-none"
                />
              </div>
            )}

            {/* Resource list */}
            <div className="max-h-[160px] overflow-y-auto py-0.5">
              {filtered.map((r) => {
                const isChecked = selectedSet.has(r.id);
                return (
                  <button
                    key={r.id}
                    type="button"
                    className="hover:bg-muted/50 flex w-full items-center gap-1.5 px-2 py-1 text-left transition-colors"
                    onClick={() => toggleResource(r.id)}
                  >
                    <div
                      className={`flex h-3 w-3 shrink-0 items-center justify-center rounded-sm border text-[8px] transition-all ${isChecked ? "bg-foreground border-foreground text-background" : "border-muted-foreground/30"}`}
                    >
                      {isChecked && "✓"}
                    </div>
                    <span className="text-foreground truncate font-mono text-[11px]">
                      {r.label}
                    </span>
                  </button>
                );
              })}
              {filtered.length === 0 && (
                <p className="text-muted-foreground px-2 py-1.5 text-center text-[11px]">
                  No matches
                </p>
              )}
            </div>
          </div>,
          document.body,
        )}
    </div>
  );
}
