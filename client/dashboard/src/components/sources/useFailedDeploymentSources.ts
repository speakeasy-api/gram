import type {
  Deployment,
  DeploymentLogEvent,
} from "@gram/client/models/components";
import {
  useDeployment,
  useDeploymentLogs,
  useLatestDeployment,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { useMemo } from "react";
import { type SourceType, attachmentToURNPrefix } from "@/lib/sources";

export interface FailedSource {
  id: string;
  name: string;
  slug: string;
  type: SourceType;
  errors: DeploymentLogEvent[];
  toolCount: number;
}

export interface UseFailedDeploymentSourcesResult {
  hasFailures: boolean;
  failedSources: FailedSource[];
  generalErrors: DeploymentLogEvent[];
  deployment: Deployment | undefined;
  isLoading: boolean;
}

export interface ComputeFailedSourcesResult {
  hasFailures: boolean;
  failedSources: FailedSource[];
  generalErrors: DeploymentLogEvent[];
}

export function useFailedDeploymentSources(
  deploymentId?: string,
): UseFailedDeploymentSourcesResult {
  const { data: latestResult, isLoading: latestLoading } = useLatestDeployment(
    undefined,
    undefined,
    {
      enabled: !deploymentId,
    },
  );

  const { data: specificResult, isLoading: specificLoading } = useDeployment(
    { id: deploymentId ?? "" },
    undefined,
    {
      enabled: !!deploymentId,
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    },
  );

  const deploymentLoading = deploymentId ? specificLoading : latestLoading;
  const deployment: Deployment | undefined = deploymentId
    ? specificResult
    : latestResult?.deployment;

  const { data: logs, isLoading: logsLoading } = useDeploymentLogs(
    { deploymentId: deployment?.id ?? "" },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      enabled: !!deployment?.id,
    },
  );

  const { data: toolsetsData } = useListToolsets();

  const result = useMemo(() => {
    if (!deployment || !logs) {
      return {
        hasFailures: false,
        failedSources: [],
        generalErrors: [],
      };
    }

    const toolUrns = flattenToolUrns(toolsetsData?.toolsets ?? []);

    return computeFailedSources({
      failedDeployment: deployment,
      compareDeployment: deployment,
      toolUrns,
      events: logs.events,
    });
  }, [deployment, logs, toolsetsData]);

  return {
    ...result,
    deployment,
    isLoading: deploymentLoading || logsLoading,
  };
}

const partition = <T>(arr: T[], pred: (x: T) => boolean): [T[], T[]] => [
  arr.filter(pred),
  arr.filter((x) => !pred(x)),
];

export function flattenToolUrns(toolsets: { toolUrns?: string[] }[]): string[] {
  return toolsets.flatMap((t) => t.toolUrns ?? []);
}

function buildSourceMap(
  deployment: Deployment,
): Map<string, { name: string; slug: string; type: SourceType }> {
  const map = new Map<
    string,
    { name: string; slug: string; type: SourceType }
  >();

  for (const asset of deployment.openapiv3Assets) {
    map.set(asset.id, { name: asset.name, slug: asset.slug, type: "openapi" });
  }
  for (const fn of deployment.functionsAssets ?? []) {
    map.set(fn.id, { name: fn.name, slug: fn.slug, type: "function" });
  }
  for (const mcp of deployment.externalMcps ?? []) {
    map.set(mcp.id, { name: mcp.name, slug: mcp.slug, type: "externalmcp" });
  }

  return map;
}

function deploymentURNPrefixes(deployment: Deployment): string[] {
  const sources = buildSourceMap(deployment);
  return [...sources.values()].map((s) =>
    attachmentToURNPrefix(s.type, s.slug),
  );
}

export function computeFailedSources(args: {
  failedDeployment: Deployment;
  compareDeployment: Deployment;
  toolUrns: string[];
  events: DeploymentLogEvent[];
}): ComputeFailedSourcesResult {
  const { failedDeployment, compareDeployment, toolUrns, events } = args;

  const errorEvents = events.filter((e) => e.event.includes("error"));

  if (errorEvents.length === 0 && failedDeployment.status !== "failed") {
    return { hasFailures: false, failedSources: [], generalErrors: [] };
  }

  const [attachmentErrors, generalErrors] = partition(
    errorEvents,
    (e) => !!e.attachmentId,
  );

  const errorsByAttachment = new Map<string, DeploymentLogEvent[]>();
  for (const event of attachmentErrors) {
    const existing = errorsByAttachment.get(event.attachmentId!) ?? [];
    existing.push(event);
    errorsByAttachment.set(event.attachmentId!, existing);
  }

  const sourceMap = buildSourceMap(failedDeployment);

  const comparePrefixes = deploymentURNPrefixes(compareDeployment);
  const relevantUrns = toolUrns.filter((urn) =>
    comparePrefixes.some((prefix) => urn.startsWith(prefix)),
  );

  const failedSources: FailedSource[] = [];
  for (const [attachmentId, errors] of errorsByAttachment) {
    const source = sourceMap.get(attachmentId);
    if (source) {
      const prefix = attachmentToURNPrefix(source.type, source.slug);
      const toolCount = relevantUrns.filter((urn) =>
        urn.startsWith(prefix),
      ).length;

      failedSources.push({
        id: attachmentId,
        ...source,
        errors,
        toolCount,
      });
    } else {
      generalErrors.push(...errors);
    }
  }

  return {
    hasFailures:
      failedDeployment.status === "failed" || failedSources.length > 0,
    failedSources,
    generalErrors,
  };
}
