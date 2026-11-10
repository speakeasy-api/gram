import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { Switch } from "@/components/ui/Switch";
import { useOrganization } from "@/contexts/Auth";
import { useListScopes } from "@gram/client/react-query/listScopes.js";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";
import { AdminSection } from "./AdminSection";
import { PlatformAdminGate } from "./PlatformAdminGate";
import {
  loadCatalog,
  saveCatalog,
  reconcileState,
  defaultScopeState,
  loadCachedResources,
  loadState,
  saveCachedResources,
  saveState,
  type OverrideState,
} from "./rbac-override-state";

export default function PlatformAdminRbacOverride(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            RBAC Override
          </Page.Section.Title>
          <Page.Section.Description>
            Simulate a reduced set of RBAC scopes for your own requests. Local
            to this browser — the override travels as a request header and never
            changes stored grants.
          </Page.Section.Description>
          <Page.Section.Body>
            <PlatformAdminGate>
              <RbacOverrideEditor />
            </PlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

export function RbacOverrideEditor(): JSX.Element {
  const [state, setState] = useState<OverrideState>(loadState);
  const [catalog, setCatalog] = useState(loadCatalog);
  const [storageError, setStorageError] = useState(false);
  const {
    data: scopesData,
    isError: catalogError,
    isPending: catalogPending,
  } = useListScopes(undefined, undefined, {
    throwOnError: false,
    retry: false,
  });
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!scopesData) return;
    try {
      const previous = loadState();
      const nextCatalog = saveCatalog(scopesData.scopes);
      const next = reconcileState(previous, nextCatalog);
      saveState(next);
      setCatalog(nextCatalog);
      setState(next);
      setStorageError(false);
      if (
        next.enabled &&
        Object.keys(next.scopes).length !== Object.keys(previous.scopes).length
      ) {
        void queryClient.invalidateQueries();
      }
      window.dispatchEvent(new Event("rbac-override-change"));
    } catch {
      setStorageError(true);
    }
  }, [scopesData, queryClient]);
  // Persist before invalidation can issue requests, not in a later effect.
  const updateState = useCallback(
    (update: (previous: OverrideState) => OverrideState) => {
      try {
        const next = update(loadState());
        saveState(next);
        setState(next);
        setStorageError(false);
      } catch {
        setStorageError(true);
      }
    },
    [],
  );
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

  // Cache the full resource list while overrides are off so the page still
  // shows every option after the user restricts scopes (a restricted
  // project:read would otherwise shrink the list to the restriction itself).
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

  const invalidate = useCallback(() => {
    setTimeout(() => {
      void queryClient.invalidateQueries();
      window.dispatchEvent(new Event("rbac-override-change"));
    }, 0);
  }, [queryClient]);

  const toggleEnabled = useCallback(() => {
    updateState((prev) => ({ ...prev, enabled: !prev.enabled }));
    invalidate();
  }, [invalidate, updateState]);

  const toggleScope = useCallback(
    (scope: string) => {
      updateState((prev) => {
        const existing = prev.scopes[scope] ?? {
          enabled: true,
          resources: null,
        };
        return {
          ...prev,
          scopes: {
            ...prev.scopes,
            [scope]: {
              ...existing,
              enabled: !existing.enabled,
              // Recovery must restore access to the active organization too.
              resources:
                scope === "org:read" && !existing.enabled
                  ? null
                  : existing.resources,
            },
          },
        };
      });
      if (state.enabled) invalidate();
    },
    [state.enabled, invalidate, updateState],
  );

  const setScopeResources = useCallback(
    (scope: string, resources: string[] | null) => {
      updateState((prev) => {
        const existing = prev.scopes[scope] ?? {
          enabled: true,
          resources: null,
        };
        return {
          ...prev,
          scopes: {
            ...prev.scopes,
            [scope]: { ...existing, resources },
          },
        };
      });
      if (state.enabled) invalidate();
    },
    [state.enabled, invalidate, updateState],
  );

  // Remounts the resource MultiSelects (which only take a defaultValue) when
  // Reset wipes their selections out from under them.
  const [resetCount, setResetCount] = useState(0);
  const reset = () => {
    updateState(() => ({ enabled: false, scopes: defaultScopeState() }));
    setResetCount((count) => count + 1);
    invalidate();
  };

  // Retain visibility of saved selections even on a cold, denied catalog fetch.
  const knownSlugs = new Set(catalog.map((scope) => scope.slug));
  const displayedCatalog = [
    ...catalog,
    ...Object.keys(state.scopes)
      .filter((slug) => !knownSlugs.has(slug))
      .map((slug) => ({
        slug,
        resourceType: "Saved scopes",
        description: "Saved selection; catalog metadata unavailable",
        visibility: "user_visible",
      })),
  ];

  const activeCount = Object.values(state.scopes).filter(
    (s) => s.enabled,
  ).length;

  return (
    <div className="space-y-8">
      <AdminSection title="Override">
        <div className="flex items-center justify-between gap-4 px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="text-foreground text-sm font-medium">
              {state.enabled ? "Override active" : "Override disabled"}
            </span>
            {state.enabled && (
              <Badge variant="warning" size="sm">
                {activeCount}/{Object.keys(state.scopes).length} scopes
              </Badge>
            )}
          </div>
          <div className="flex items-center gap-3">
            <Button variant="tertiary" size="sm" onClick={reset}>
              Reset
            </Button>
            <Switch
              checked={state.enabled}
              onCheckedChange={toggleEnabled}
              aria-label="Toggle RBAC override"
            />
          </div>
        </div>
      </AdminSection>

      {storageError && (
        <p role="alert">
          Unable to save the override. Allow browser storage or free space, then
          retry. Your previous selections remain active.
        </p>
      )}
      {(catalogError || catalogPending) && (
        <p role="status" className="text-muted-foreground text-sm">
          {catalogError
            ? "Scope catalog unavailable."
            : "Loading scope catalog."}{" "}
          Saved selections and cached scopes are preserved. The catalog requires
          org:read. Enable org:read below to restore access for the active
          organization, or disable the override.
        </p>
      )}
      <div
        className={
          state.enabled
            ? "space-y-8"
            : "space-y-8 pointer-events-none opacity-40"
        }
      >
        {Array.from(
          new Set(displayedCatalog.map((scope) => scope.resourceType)),
        ).map((group) => {
          const scopes = displayedCatalog.filter(
            (scope) => scope.resourceType === group,
          );
          return (
            <AdminSection
              key={group}
              title={
                group === "org" ? "Organization" : group.replaceAll("_", " ")
              }
            >
              <div className="divide-border divide-y">
                {scopes.map((def) => {
                  const scopeState = state.scopes[def.slug] ?? {
                    enabled: true,
                    resources: null,
                  };
                  // Defensive: legacy localStorage may have entries without
                  // `resources`. Loose != null so null and undefined both skip
                  // the length read.
                  const isRestricted =
                    scopeState.resources != null &&
                    scopeState.resources.length > 0;
                  let knownResources: { id: string; label: string }[] = [];
                  if (
                    def.resourceType === "project" ||
                    def.resourceType === "skill" ||
                    def.resourceType === "assistant"
                  ) {
                    knownResources = projectResources;
                  } else if (def.resourceType === "mcp") {
                    knownResources = mcpResources;
                  }

                  return (
                    <div key={def.slug} className="px-4 py-3">
                      <label className="flex cursor-pointer items-center gap-3">
                        <Checkbox
                          checked={scopeState.enabled}
                          onCheckedChange={() => toggleScope(def.slug)}
                          aria-label={`Toggle ${def.slug}`}
                        />
                        <span
                          className={`font-mono text-sm ${
                            scopeState.enabled
                              ? "text-foreground"
                              : "text-muted-foreground line-through"
                          }`}
                        >
                          {def.slug}
                        </span>
                        {isRestricted && scopeState.enabled && (
                          <Badge variant="information" size="sm">
                            scoped
                          </Badge>
                        )}
                        <span className="text-muted-foreground ml-auto text-sm">
                          {def.description}
                        </span>
                      </label>

                      {scopeState.enabled && knownResources.length > 0 && (
                        <div className="mt-2 pl-7">
                          <MultiSelect
                            key={`${def.slug}-${resetCount}`}
                            options={knownResources.map((r) => ({
                              label: r.label,
                              value: r.id,
                            }))}
                            defaultValue={scopeState.resources ?? []}
                            onValueChange={(resources) =>
                              setScopeResources(
                                def.slug,
                                resources.length > 0 ? resources : null,
                              )
                            }
                            placeholder="All resources (wildcard)"
                            className="max-w-md"
                          />
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </AdminSection>
          );
        })}
      </div>
    </div>
  );
}
