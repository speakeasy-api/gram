import { CommandGroup, CommandItem } from "@/components/ui/command";
import { useRBAC } from "@/hooks/useRBAC";
import { useEnvironments } from "@/pages/environments/useEnvironments";
import { useToolsets } from "@/pages/toolsets/useToolsets";
import { useRoutes } from "@/routes";
import {
  useAssistantsList,
  useLatestDeployment,
  useListDeploymentsSuspense,
  useRiskListCustomDetectionRules,
  useRiskListPolicies,
} from "@gram/client/react-query/index.js";
import { usePluginsSuspense } from "@gram/client/react-query/plugins";
import { useShadowMCPApprovalRequestsSuspense } from "@gram/client/react-query/shadowMCPApprovalRequests.js";
import { Icon, type IconName } from "@speakeasy-api/moonshine";
import { Suspense, useMemo, type ReactNode } from "react";
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
  const toolsets = useToolsets();
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
  const { data } = useLatestDeployment();
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
  const { data } = useAssistantsList(undefined, undefined, {
    retry: false,
    throwOnError: false,
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
  const { data } = useRiskListPolicies();
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
            // No per-policy detail route; land on the Risk Policies page.
            routes.policyCenter.goTo();
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function DetectionRulesGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
  const { data } = useRiskListCustomDetectionRules();
  const rules = data?.rules ?? [];
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
            // No per-rule detail route; land on the Detection Rules page.
            routes.detectionRules.goTo();
            onNavigate();
          }}
        />
      ))}
    </CommandGroup>
  );
}

function ApprovalRequestsGroup({ onNavigate }: GroupProps) {
  const routes = useRoutes();
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
              // No per-request detail route; land on the Approval Requests page.
              routes.approvalRequests.goTo();
              onNavigate();
            }}
          />
        );
      })}
    </CommandGroup>
  );
}

export function ResourceResults({ onNavigate }: GroupProps): JSX.Element {
  const { hasAnyScope } = useRBAC();
  // Risk resources are org:admin-gated on their own pages; mirror that here so
  // non-admins never fire the (forbidden) list calls.
  const isAdmin = hasAnyScope(["org:admin"]);

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
          <LazyGroup>
            <DetectionRulesGroup onNavigate={onNavigate} />
          </LazyGroup>
          <LazyGroup>
            <ApprovalRequestsGroup onNavigate={onNavigate} />
          </LazyGroup>
        </>
      )}
    </>
  );
}
