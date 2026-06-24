import { CommandGroup, CommandItem } from "@/components/ui/command";
import { useRBAC } from "@/hooks/useRBAC";
import { useEnvironments } from "@/pages/environments/useEnvironments";
import { BUILTIN_RULES_BY_CATEGORY } from "@/pages/security/detection-rules-data";
import { useRoutes } from "@/routes";
import {
  useAssistantsListSuspense,
  useLatestDeploymentSuspense,
  useListDeploymentsSuspense,
  useListToolsetsSuspense,
  useRiskListCustomDetectionRulesSuspense,
  useRiskListPoliciesSuspense,
} from "@gram/client/react-query/index.js";
import { usePluginsSuspense } from "@gram/client/react-query/plugins";
import { useShadowMCPApprovalRequestsSuspense } from "@gram/client/react-query/shadowMCPApprovalRequests.js";
import { Icon, type IconName } from "@speakeasy-api/moonshine";
import { Suspense, useMemo, type ReactNode } from "react";
import { useNavigate } from "react-router";
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

function McpServersGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const { data } = useListToolsetsSuspense();
  const toolsets = data.toolsets ?? [];
  if (!toolsets.length) return null;
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
            routes.sources.source.goTo(source.kind, source.slug);
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
  const { data } = usePluginsSuspense();
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
    <CommandGroup heading="Risk Policies">
      {policies.map((policy) => (
        <ResultItem
          key={policy.id}
          value={`risk policy ${policy.name} ${policy.id}`}
          label={policy.name}
          icon="shield-check"
          onSelect={() => {
            // Drill into the policy's detail view (AGE-2704).
            void navigate(routes.policyDetail.href(policy.id));
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
            // No per-rule route; deep-link opens the rule's sheet by id.
            void navigate(
              `${routes.detectionRules.href()}?rule=${encodeURIComponent(rule.id)}`,
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
  const { data } = useShadowMCPApprovalRequestsSuspense({
    status: "requested",
  });
  const requests = data?.requests ?? [];
  if (!requests.length) return null;
  return (
    <CommandGroup heading="Approval Requests">
      {requests.map((request) => {
        const label =
          request.observedName ??
          request.toolName ??
          request.requesterDisplayName ??
          request.observedServerIdentity ??
          request.id;
        return (
          <ResultItem
            key={request.id}
            value={`approval request ${label} ${request.id}`}
            label={label}
            sublabel={request.status}
            icon="inbox"
            onSelect={() => {
              // No per-request route; deep-link opens the review sheet by id.
              void navigate(
                `${routes.approvalRequests.href()}?review=${encodeURIComponent(request.id)}`,
              );
              onNavigate();
            }}
          />
        );
      })}
    </CommandGroup>
  );
}

export function ResourceResults({
  onNavigate,
  query,
}: GroupProps & { query: string }): JSX.Element {
  const { hasAnyScope } = useRBAC();
  // Risk resources are org:admin-gated on their own pages; mirror that here so
  // non-admins never fire the (forbidden) list calls.
  const isAdmin = hasAnyScope(["org:admin"]);
  // Detection rules are high-cardinality (dozens of built-ins), so they'd flood
  // the default view and fetch on open. Make them search-only: render (and
  // fetch) the group only once the user types, letting cmdk filter the results.
  const hasQuery = query.length > 0;

  return (
    <>
      <LazyGroup>
        <McpServersGroup onNavigate={onNavigate} />
      </LazyGroup>
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
      <LazyGroup>
        <PluginsGroup onNavigate={onNavigate} />
      </LazyGroup>
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
          <LazyGroup>
            <ApprovalRequestsGroup onNavigate={onNavigate} />
          </LazyGroup>
        </>
      )}
    </>
  );
}
