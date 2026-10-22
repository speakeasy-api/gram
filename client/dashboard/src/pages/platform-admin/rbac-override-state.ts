// Shared by the editor and request-header consumer, including before editor mount.
const RBAC_OVERRIDE_STORAGE_KEY = "gram-rbac-dev-override";
const CATALOG_KEY = "gram-rbac-dev-scope-catalog";
export type CatalogScope = {
  slug: string;
  resourceType: string;
  description: string;
  visibility: string;
};
const RECOVERY_SCOPE: CatalogScope = {
  slug: "org:read",
  resourceType: "org",
  description: "Read organization metadata and the scope catalog",
  visibility: "user_visible",
};
function visibleCatalog(scopes: CatalogScope[]): CatalogScope[] {
  return [
    RECOVERY_SCOPE,
    ...scopes.filter(
      (scope) =>
        scope.visibility === "user_visible" && scope.slug !== "org:read",
    ),
  ];
}
export function loadCatalog(): CatalogScope[] {
  try {
    const raw = localStorage.getItem(CATALOG_KEY);
    if (raw) return visibleCatalog(JSON.parse(raw));
  } catch {
    /* A bad cache must not hide recovery. */
  }
  return [RECOVERY_SCOPE];
}
export function saveCatalog(scopes: CatalogScope[]): CatalogScope[] {
  const catalog = visibleCatalog(scopes);
  try {
    localStorage.setItem(CATALOG_KEY, JSON.stringify(catalog));
  } catch {
    // Catalog caching is best-effort; persisted choices remain authoritative.
  }
  return catalog;
}
function normalizeScope(value: unknown): ScopeState {
  if (typeof value === "boolean") return { enabled: value, resources: null };
  if (
    value &&
    typeof value === "object" &&
    "enabled" in value &&
    typeof value.enabled === "boolean"
  ) {
    const resources = "resources" in value ? value.resources : null;
    if (
      resources == null ||
      (Array.isArray(resources) &&
        resources.every((id) => typeof id === "string"))
    ) {
      return { enabled: value.enabled, resources: resources ?? null };
    }
  }
  // Corrupt entries must not make header serialization fall back to full access.
  return { enabled: false, resources: null };
}
export type ScopeState = { enabled: boolean; resources: string[] | null };
export type OverrideState = {
  enabled: boolean;
  scopes: Record<string, ScopeState>;
};
export function defaultScopeState(
  catalog = loadCatalog(),
): Record<string, ScopeState> {
  return Object.fromEntries(
    catalog.map((scope) => [scope.slug, { enabled: true, resources: null }]),
  );
}
export function reconcileState(
  state: OverrideState,
  catalog = loadCatalog(),
): OverrideState {
  // Keep saved scopes even when the catalog is unavailable or changes.
  return {
    ...state,
    scopes: { ...defaultScopeState(catalog), ...state.scopes },
  };
}
export function loadState(): OverrideState {
  try {
    const raw = localStorage.getItem(RBAC_OVERRIDE_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      const scopes = Object.fromEntries(
        Object.entries(parsed.scopes ?? {}).map(([scope, value]) => [
          scope,
          normalizeScope(value),
        ]),
      ) as Record<string, ScopeState>;
      return reconcileState({ enabled: parsed.enabled ?? false, scopes });
    }
  } catch {
    /* Ignore malformed localStorage. */
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
