import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { Switch } from "@/components/ui/Switch";
import { useOrganization } from "@/contexts/Auth";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";
import { AdminSection } from "./AdminSection";
import { PlatformAdminGate } from "./PlatformAdminGate";
import {
  GROUP_ORDER,
  SCOPE_DEFS,
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

function RbacOverrideEditor(): JSX.Element {
  const [state, setState] = useState<OverrideState>(loadState);
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

  useEffect(() => {
    saveState(state);
  }, [state]);

  const invalidate = useCallback(() => {
    setTimeout(() => {
      void queryClient.invalidateQueries();
      window.dispatchEvent(new Event("rbac-override-change"));
    }, 0);
  }, [queryClient]);

  const toggleEnabled = useCallback(() => {
    setState((prev) => ({ ...prev, enabled: !prev.enabled }));
    invalidate();
  }, [invalidate]);

  const toggleScope = useCallback(
    (scope: string) => {
      setState((prev) => {
        const existing = prev.scopes[scope] ?? {
          enabled: true,
          resources: null,
        };
        return {
          ...prev,
          scopes: {
            ...prev.scopes,
            [scope]: { ...existing, enabled: !existing.enabled },
          },
        };
      });
      if (state.enabled) invalidate();
    },
    [state.enabled, invalidate],
  );

  const setScopeResources = useCallback(
    (scope: string, resources: string[] | null) => {
      setState((prev) => {
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
    [state.enabled, invalidate],
  );

  // Remounts the resource MultiSelects (which only take a defaultValue) when
  // Reset wipes their selections out from under them.
  const [resetCount, setResetCount] = useState(0);
  const reset = () => {
    setState({ enabled: false, scopes: defaultScopeState() });
    setResetCount((count) => count + 1);
    invalidate();
  };

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
                {activeCount}/{SCOPE_DEFS.length} scopes
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

      <div
        className={
          state.enabled
            ? "space-y-8"
            : "space-y-8 pointer-events-none opacity-40"
        }
      >
        {GROUP_ORDER.map((group) => {
          const scopes = SCOPE_DEFS.filter((s) => s.resourceType === group.key);
          return (
            <AdminSection key={group.key} title={group.label}>
              <div className="divide-border divide-y">
                {scopes.map((def) => {
                  const scopeState = state.scopes[def.scope] ?? {
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
                    def.resourceType === "skill"
                  ) {
                    knownResources = projectResources;
                  } else if (def.resourceType === "mcp") {
                    knownResources = mcpResources;
                  }

                  return (
                    <div key={def.scope} className="px-4 py-3">
                      <label className="flex cursor-pointer items-center gap-3">
                        <Checkbox
                          checked={scopeState.enabled}
                          onCheckedChange={() => toggleScope(def.scope)}
                          aria-label={`Toggle ${def.scope}`}
                        />
                        <span
                          className={`font-mono text-sm ${
                            scopeState.enabled
                              ? "text-foreground"
                              : "text-muted-foreground line-through"
                          }`}
                        >
                          {def.label}
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
                            key={`${def.scope}-${resetCount}`}
                            options={knownResources.map((r) => ({
                              label: r.label,
                              value: r.id,
                            }))}
                            defaultValue={scopeState.resources ?? []}
                            onValueChange={(resources) =>
                              setScopeResources(
                                def.scope,
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
