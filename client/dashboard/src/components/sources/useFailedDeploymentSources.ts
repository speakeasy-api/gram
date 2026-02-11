import type {
  Deployment,
  DeploymentLogEvent,
} from "@gram/client/models/components";
import {
  useDeploymentLogs,
  useLatestDeployment,
} from "@gram/client/react-query/index.js";
import { useMemo } from "react";

export interface FailedSource {
  id: string;
  name: string;
  slug: string;
  type: "openapi" | "function" | "externalmcp";
  errors: DeploymentLogEvent[];
}

export interface UseFailedDeploymentSourcesResult {
  hasFailures: boolean;
  failedSources: FailedSource[];
  generalErrors: DeploymentLogEvent[];
  deployment: Deployment | undefined;
  isLoading: boolean;
}

export function useFailedDeploymentSources(): UseFailedDeploymentSourcesResult {
  const { data: deploymentResult, isLoading: deploymentLoading } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

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

  const result = useMemo(() => {
    if (!deployment || !logs) {
      return {
        hasFailures: false,
        failedSources: [],
        generalErrors: [],
      };
    }

    // Filter to error events (matching pattern from useDeploymentLogsSummary)
    const errorEvents = logs.events.filter((e) => e.event.includes("error"));

    if (errorEvents.length === 0 && deployment.status !== "failed") {
      return {
        hasFailures: false,
        failedSources: [],
        generalErrors: [],
      };
    }

    // Group errors by attachmentId
    const errorsByAttachment = new Map<string, DeploymentLogEvent[]>();
    const generalErrors: DeploymentLogEvent[] = [];

    for (const event of errorEvents) {
      if (event.attachmentId) {
        const existing = errorsByAttachment.get(event.attachmentId) ?? [];
        existing.push(event);
        errorsByAttachment.set(event.attachmentId, existing);
      } else {
        generalErrors.push(event);
      }
    }

    // Build a lookup from source ID to source info
    const sourceMap = new Map<
      string,
      { name: string; slug: string; type: FailedSource["type"] }
    >();

    for (const asset of deployment.openapiv3Assets) {
      sourceMap.set(asset.id, {
        name: asset.name,
        slug: asset.slug,
        type: "openapi",
      });
    }
    for (const fn of deployment.functionsAssets ?? []) {
      sourceMap.set(fn.id, {
        name: fn.name,
        slug: fn.slug,
        type: "function",
      });
    }
    for (const mcp of deployment.externalMcps ?? []) {
      sourceMap.set(mcp.id, {
        name: mcp.name,
        slug: mcp.slug,
        type: "externalmcp",
      });
    }

    // Match errors to sources
    const failedSources: FailedSource[] = [];
    for (const [attachmentId, errors] of errorsByAttachment) {
      const source = sourceMap.get(attachmentId);
      if (source) {
        failedSources.push({
          id: attachmentId,
          ...source,
          errors,
        });
      } else {
        // Attachment doesn't match a known source — treat as general errors
        generalErrors.push(...errors);
      }
    }

    return {
      hasFailures: deployment.status === "failed" || failedSources.length > 0,
      failedSources,
      generalErrors,
    };
  }, [deployment, logs]);

  return {
    ...result,
    deployment,
    isLoading: deploymentLoading || logsLoading,
  };
}
