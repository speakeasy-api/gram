import { useOrganization, useProject } from "@/contexts/Auth";
import { usePluginWriteAccess } from "@/hooks/usePluginWriteAccess";
import { usePluginQueryScope } from "@/pages/plugins/usePluginQueryScope";
import { CommandGroup, CommandItem } from "@/components/ui/Command";
import { useProjectSlugForRequests, useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { mcpServerRouteParam } from "@/lib/sources";
import { useEnvironments } from "@/pages/environments/useEnvironments";
import { CATALOG_STALE_TIME_MS } from "@/pages/catalog/hooks";
import { BUILTIN_RULES_BY_CATEGORY } from "@/pages/security/detection-rules-data";
import { encodeIdentityUrn, withIdentityWindow } from "@/lib/identity-urn";
import { useRoutes } from "@/routes";
import { useAssistantsListSuspense } from "@gram/client/react-query/assistantsList.js";
import { useLatestDeploymentSuspense } from "@gram/client/react-query/latestDeployment.js";
import { useListDeploymentsSuspense } from "@gram/client/react-query/listDeployments.js";
import { useListMCPCatalogSuspense } from "@gram/client/react-query/listMCPCatalog.js";
import { useListToolsetsSuspense } from "@gram/client/react-query/listToolsets.js";
import { useMcpServersSuspense } from "@gram/client/react-query/mcpServers.js";
import { useMembersSuspense } from "@gram/client/react-query/members.js";
import { useRiskListCustomDetectionRulesSuspense } from "@gram/client/react-query/riskListCustomDetectionRules.js";
import { useListMcpApprovalRequestsSuspense } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { useRiskListPoliciesSuspense } from "@gram/client/react-query/riskListPolicies.js";
import { usePluginsSuspense } from "@gram/client/react-query/plugins";
import { Icon } from "@/components/ui/Icon";
import { type IconName } from "@/components/ui/Icon/names";
import { Suspense, useMemo, type ReactNode } from "react";
import { useLocation, useNavigate } from "react-router";
import { CommandErrorBoundary } from "./CommandErrorBoundary";

/**
 * Resource search results for the command palette.
 *
 * Each resource type is its own component calling its existing list hook, so
 * Rules-of-Hooks are respected and each group can be fetched and fault-isolated
 * independently. Groups are wrapped in <LazyGroup> (Suspense + error boundary)
 * so they pop in as their data resolves and one failing endpoint can't blank
 * the palette. This whole tree is only mounted while the palette is open
 * (see CommandPalette), which is what makes the fetches lazy.
 *
 * cmdk filters every rendered CommandItem against the typed query using each
 * item's `value`, so we fold name/slug/id into `value` for broad matching while
 * keeping the visible label clean. We pass unfiltered lists to cmdk — only one
 * filter is ever active, avoiding the double-filter inconsistency (AIS-84).
 */

interface GroupProps {
  /** Called after navigating, to close the palette. */
  onNavigate: () => void;
}

function LazyGroup({ children }: { children: ReactNode }) {
  return (
    <CommandErrorBoundary>
      <Suspense fallback={null}>{children}</Suspense>
    </CommandErrorBoundary>
  );
}

function ResultItem({
  value,
  label,
  sublabel,
  icon,
  onSelect,
}: {
  value: string;
  label: string;
  sublabel?: string;
  icon?: IconName;
  onSelect: () => void;
}) {
  return (
    <CommandItem
      value={value}
      onSelect={onSelect}
      className="flex items-center justify-between"
    >
      <div className="flex min-w-0 items-center gap-2">
        {icon && <Icon name={icon} className="size-4 shrink-0" />}
        <span className="truncate">{label}</span>
      </div>
      {sublabel && (
        <span className="text-muted-foreground ml-2 shrink-0 text-xs">
          {sublabel}
        </span>
      )}
    </CommandItem>
  );
}

// TODO(AGE-1902): collapse the two fetches once Hosted (toolset-backed) MCP
// servers also source from mcp_servers.
//
// The palette mirrors the /mcp listing (see pages/mcp/MCP.tsx): "MCP Servers"
// is one user-facing collection assembled from two backing stores, so both are
// searched under a single heading. mcp_servers rows are filtered to the
// non-toolset backends for the same reason the listing does it — a
// toolset-backed mcp_servers row is the *same* server the toolsets fetch
// already returned, so including it would double every hosted server in the
// results. With that filter the two sets are disjoint and no dedupe is needed.
//
// Both fetches share this group's Suspense/error boundary, so either failing
// hides the whole group. That's deliberate: half an "MCP Servers" list is worse
// than none, because a user who searches and finds nothing concludes the server
// doesn't exist.
function McpServersGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();
  // Both lists are keyed by project: the SDK folds gramProject into the query
  // key, so omitting it shares one cache entry across every project and a
  // switch renders the previous project's rows until the refetch lands.
  const { data: toolsetsData } = useListToolsetsSuspense({ gramProject });
  const { data: mcpServersData } = useMcpServersSuspense({ gramProject });
  const toolsets = toolsetsData.toolsets ?? [];
  const mcpServers = useMemo(
    () =>
      (mcpServersData.mcpServers ?? []).filter(
        (server) =>
          !!server.remoteMcpServerId ||
          !!server.tunneledMcpServerId ||
          !!server.unproxiedMcpServerId,
      ),
    [mcpServersData],
  );
  if (!toolsets.length && !mcpServers.length) return null;
  return (
    <CommandGroup heading="MCP Servers">
      {toolsets.map((toolset) => (
        <ResultItem
          key={toolset.id}
          value={`mcp ${toolset.name} ${toolset.slug} ${toolset.id}`}
          label={toolset.name}
          sublabel={toolset.slug}
          icon="network"
          onSelect={() => {
            routes.mcp.details.goTo(toolset.slug);
            onNavigate();
          }}
        />
      ))}
      {mcpServers.map((server) => (
        <ResultItem
          key={server.id}
          value={`mcp ${server.name ?? ""} ${server.slug ?? ""} ${server.id}`}
          label={server.name || "MCP Server"}
          sublabel={server.slug}
          icon="network"
          onSelect={() => {
            routes.mcp.x.overview.goTo(mcpServerRouteParam(server));
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

/**
 * Third-party servers offered by the registry catalog.
 *
 * Kept in a group of its own, under its own icon, rather than folded into "MCP
 * Servers": a catalog hit is something this project could run, not something it
 * runs, and the two legitimately share a name once an entry has been added. The
 * registry specifier rides along as the sublabel, so a row is never mistaken
 * for one of the project's own slugs.
 *
 * The typed query filters the fetched list here rather than being sent to
 * listCatalog, which keeps what this group can reach identical to what the
 * catalog page can show. Both read the same capped response: listCatalog
 * concatenates its sources in priority order and truncates the merged list, so
 * the day a second source is enabled, entries past the cap fall out of the
 * page, out of this group, and out of the detail page — which resolves a
 * selected row by finding it in that same list. Searching server-side here
 * alone would filter per source before the merge and so surface rows the
 * detail page then fails to resolve. The cap is the thing to lift, and the
 * handler already marks it as standing until cursor pagination lands.
 */
function McpCatalogGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const gramProject = useProjectSlugForRequests();
  // Same request the catalog page makes, down to the freshness window: staleness
  // is per-observer, so without it this reader would refetch the registry on
  // every palette mount despite sharing a cache entry the page still considers
  // fresh.
  const { data } = useListMCPCatalogSuspense({ gramProject }, undefined, {
    staleTime: CATALOG_STALE_TIME_MS,
  });
  // A specifier is unique within a registry but not across them, and the detail
  // route is addressed by specifier alone — it resolves the first entry that
  // matches. Two registries publishing one server would otherwise render as two
  // identical rows that lead to the same page, so only the row that page
  // actually opens is offered.
  const servers = useMemo(() => {
    const bySpecifier = new Map<string, (typeof data.servers)[number]>();
    for (const server of data.servers ?? []) {
      if (!bySpecifier.has(server.registrySpecifier)) {
        bySpecifier.set(server.registrySpecifier, server);
      }
    }
    return Array.from(bySpecifier.values());
  }, [data]);
  if (!servers.length) return null;
  return (
    <CommandGroup heading="MCP Catalog">
      {servers.map((server) => (
        <ResultItem
          key={server.registrySpecifier}
          value={`catalog ${server.title ?? ""} ${server.registrySpecifier}`}
          label={server.title || server.registrySpecifier}
          // Dropped when it is standing in as the label: an untitled entry
          // would otherwise print its specifier twice on the same row.
          sublabel={server.title ? server.registrySpecifier : undefined}
          icon="store"
          onSelect={() => {
            routes.mcp.catalog.detail.goTo(
              encodeURIComponent(server.registrySpecifier),
            );
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function SourcesGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const { data } = useLatestDeploymentSuspense();
  const deployment = data?.deployment;

  const sources = useMemo(() => {
    if (!deployment) return [];
    return [
      ...deployment.openapiv3Assets.map((asset) => ({
        name: asset.name,
        slug: asset.slug,
        kind: "openapi" as const,
      })),
      ...(deployment.functionsAssets ?? []).map((asset) => ({
        name: asset.name,
        slug: asset.slug,
        kind: "function" as const,
      })),
      ...(deployment.externalMcps ?? []).map((asset) => ({
        name: asset.name,
        slug: asset.slug,
        kind: "externalmcp" as const,
      })),
    ];
  }, [deployment]);

  if (!sources.length) return null;
  return (
    <CommandGroup heading="Sources">
      {sources.map((source) => (
        <ResultItem
          key={`${source.kind}/${source.slug}`}
          value={`source ${source.name} ${source.slug} ${source.kind}`}
          label={source.name}
          sublabel={source.kind}
          icon="file-code"
          onSelect={() => {
            routes.mcp.goTo();
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function DeploymentsGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const { data } = useListDeploymentsSuspense();
  const deployments = data.items ?? [];
  if (!deployments.length) return null;
  return (
    <CommandGroup heading="Deployments">
      {deployments.map((deployment) => (
        <ResultItem
          key={deployment.id}
          value={`deployment ${deployment.id} ${deployment.status}`}
          label={`Deployment ${deployment.id.slice(0, 8)}`}
          sublabel={deployment.status}
          icon="history"
          onSelect={() => {
            routes.deployments.deployment.goTo(deployment.id);
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function EnvironmentsGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const environments = useEnvironments();
  if (!environments.length) return null;
  return (
    <CommandGroup heading="Environments">
      {environments.map((environment) => (
        <ResultItem
          key={environment.id}
          value={`environment ${environment.name} ${environment.slug} ${environment.id}`}
          label={environment.name}
          sublabel={environment.slug}
          icon="blocks"
          onSelect={() => {
            routes.environments.environment.goTo(environment.slug);
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function AssistantsGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const { data } = useAssistantsListSuspense(undefined, undefined, {
    retry: false,
  });
  const assistants = data?.assistants ?? [];
  if (!assistants.length) return null;
  return (
    <CommandGroup heading="Assistants">
      {assistants.map((assistant) => (
        <ResultItem
          key={assistant.id}
          value={`assistant ${assistant.name} ${assistant.id}`}
          label={assistant.name}
          sublabel={assistant.status}
          icon="bot"
          onSelect={() => {
            routes.assistants.detail.goTo(assistant.id);
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function PluginsGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const scope = usePluginQueryScope();
  const { data } = usePluginsSuspense(scope);
  const plugins = data?.plugins ?? [];
  if (!plugins.length) return null;
  return (
    <CommandGroup heading="Plugins">
      {plugins.map((plugin) => (
        <ResultItem
          key={plugin.id}
          value={`plugin ${plugin.name} ${plugin.slug} ${plugin.id}`}
          label={plugin.name}
          sublabel={plugin.slug}
          icon="puzzle"
          onSelect={() => {
            routes.plugins.detail.goTo(plugin.id);
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function RiskPoliciesGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data } = useRiskListPoliciesSuspense();
  const policies = data?.policies ?? [];
  if (!policies.length) return null;
  return (
    <CommandGroup heading="Guardrails">
      {policies.map((policy) => (
        <ResultItem
          key={policy.id}
          value={`risk policy ${policy.name} ${policy.id}`}
          label={policy.name}
          icon="shield-check"
          onSelect={() => {
            // No per-policy route; deep-link opens the policy's sheet by id.
            void navigate(
              `${routes.policyCenter.href()}?policy=${encodeURIComponent(policy.id)}`,
            );
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function DetectionRulesGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { data } = useRiskListCustomDetectionRulesSuspense();

  // Surface both built-in rules (static) and custom rules (from the API). The
  // detail page's `?rule=<id>` deep link tells them apart via the `custom.` id
  // prefix, so a single param drives both.
  const rules = useMemo(() => {
    const builtin = Object.values(BUILTIN_RULES_BY_CATEGORY)
      .flat()
      .map((rule) => ({
        id: rule.id,
        title: rule.title,
        severity: rule.defaultSeverity as string,
      }));
    const custom = (data?.rules ?? []).map((rule) => ({
      id: rule.id,
      title: rule.title,
      severity: rule.severity as string,
    }));
    return [...builtin, ...custom];
  }, [data]);

  if (!rules.length) return null;
  return (
    <CommandGroup heading="Detection Rules">
      {rules.map((rule) => (
        <ResultItem
          key={rule.id}
          value={`detection rule ${rule.title} ${rule.id}`}
          label={rule.title}
          sublabel={rule.severity}
          icon="scan-search"
          onSelect={() => {
            // No per-rule route; deep-link opens the rule's sheet by id on
            // the Guardrails page's Detection Rules tab.
            void navigate(
              `${routes.policyCenter.href()}?tab=detection-rules&rule=${encodeURIComponent(rule.id)}`,
            );
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function ApprovalRequestsGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { projectSlug = "" } = useSlugs();
  const { data } = useListMcpApprovalRequestsSuspense({
    status: "requested",
    gramProject: projectSlug,
  });
  const requests = data?.requests ?? [];
  if (!requests.length) return null;
  return (
    <CommandGroup heading="Access Requests">
      {requests.map((request) => (
        <ResultItem
          key={request.id}
          value={`access request ${request.targetRaw} ${request.id}`}
          label={request.targetRaw}
          sublabel={request.status}
          icon="inbox"
          onSelect={() => {
            // stdio targets have no server page; their row on the servers
            // table opens the review sheet.
            void navigate(
              request.serverSlug
                ? routes.shadowMCP.detail.href(request.serverSlug)
                : routes.shadowMCP.href(),
            );
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

/**
 * People, by name or address, jumping straight to their identity page.
 *
 * Directory members only: the identities index also lists unattributed
 * addresses and agent ids, but reaching those needs an all-time telemetry crawl
 * — far too heavy for a surface that has to answer on every keystroke.
 */
function PeopleGroup({ onNavigate }: GroupProps) {
  // The identity page lives under a project, and the palette opens from the
  // org shell too, where the path carries no slug. Fall back to the slug those
  // pages already send on their requests, the same way IdentityLink does —
  // without it the palette built `/org/projects//identities/...`, which
  // matches no route.
  const projectSlug = useProjectSlugForRequests();
  const routes = useRoutes({ projectSlug });
  const navigate = useNavigate();
  // The palette opens over whatever page the reader had narrowed, so the
  // person's page opens on that same window rather than the default one.
  const { search } = useLocation();
  const { data } = useMembersSuspense();
  const members = data?.members ?? [];
  if (!members.length) return null;
  return (
    <CommandGroup heading="People">
      {members.map((member) => (
        <ResultItem
          key={member.id}
          value={`person ${member.name} ${member.email} ${member.id}`}
          label={member.name || member.email}
          sublabel={member.email}
          icon="user"
          onSelect={() => {
            void navigate(
              withIdentityWindow(
                routes.identities.detail.overview.href(
                  encodeIdentityUrn(`user:${member.id}`),
                ),
                search,
              ),
            );
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

/**
 * The people group on its own, so the palette can offer it from the org shell
 * too — where the project-scoped resource groups have no project to read.
 */
export function PeopleResults({ onNavigate }: GroupProps): JSX.Element | null {
  const organization = useOrganization();
  const { hasAnyScope } = useRBAC();
  if (!hasAnyScope(["org:read", "org:admin"], organization.id)) return null;
  return (
    <LazyGroup>
      <PeopleGroup onNavigate={onNavigate} />
    </LazyGroup>
  );
}

export function ResourceResults({
  onNavigate,
  query,
}: GroupProps & { query: string }): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const { hasAnyScope, hasScope } = useRBAC();
  // Risk resources are org:admin-gated on their own pages; mirror that here so
  // non-admins never fire the (forbidden) list calls.
  const canWritePlugins = usePluginWriteAccess();
  const canReadPlugins =
    canWritePlugins || hasAnyScope(["org:read", "org:admin"], organization.id);
  const isAdmin = hasAnyScope(["org:admin"], organization.id);
  // Approval requests are an org-admin surface, matching the queue page's
  // own gate.
  const canReadApprovals = hasScope("org:admin", organization.id);
  // What listCatalog itself requires, rather than the looser any-of gate the
  // catalog page renders behind: an mcp:write-only reader would pass that one
  // and then have the request refused.
  const canBrowseCatalog = hasScope("project:read", project.id);
  // Detection rules and the catalog are high-cardinality (dozens of built-ins;
  // hundreds of registry entries), so they'd flood the default view and fetch
  // on open. Make them search-only: render (and fetch) the group only once the
  // user types, letting cmdk filter the results.
  const hasQuery = query.length > 0;

  return (
    <>
      <LazyGroup>
        <McpServersGroup onNavigate={onNavigate} />
      </LazyGroup>
      {/* Directly below the project's own servers: when a name matches both,
          what you already run should read first and the catalog offer second. */}
      {canBrowseCatalog && hasQuery && (
        <LazyGroup>
          <McpCatalogGroup onNavigate={onNavigate} />
        </LazyGroup>
      )}
      <LazyGroup>
        <SourcesGroup onNavigate={onNavigate} />
      </LazyGroup>
      <LazyGroup>
        <DeploymentsGroup onNavigate={onNavigate} />
      </LazyGroup>
      <LazyGroup>
        <EnvironmentsGroup onNavigate={onNavigate} />
      </LazyGroup>
      <LazyGroup>
        <AssistantsGroup onNavigate={onNavigate} />
      </LazyGroup>
      {canReadPlugins && (
        <LazyGroup>
          <PluginsGroup onNavigate={onNavigate} />
        </LazyGroup>
      )}
      {isAdmin && (
        <>
          <LazyGroup>
            <RiskPoliciesGroup onNavigate={onNavigate} />
          </LazyGroup>
          {hasQuery && (
            <LazyGroup>
              <DetectionRulesGroup onNavigate={onNavigate} />
            </LazyGroup>
          )}
        </>
      )}
      {canReadApprovals && (
        <LazyGroup>
          <ApprovalRequestsGroup onNavigate={onNavigate} />
        </LazyGroup>
      )}
    </>
  );
}
