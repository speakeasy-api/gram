import { PublicMcpWarningDialog } from "@/components/public-mcp-warning-dialog";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import { useToolset } from "@/hooks/toolTypes";
import { getSystemProvidedVariables } from "@/pages/mcp/environmentVariableUtils";
import { useEnvironmentVariables } from "@/pages/mcp/useEnvironmentVariables";
import type { McpServerVisibility } from "@gram/client/models/components/mcpserver.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import React, { useMemo, useState } from "react";

/**
 * The confirmations a hosted server's visibility change goes through: a
 * warning before going public with system-provided variables, and the enable
 * dialog (with its free-tier gate) when enabling or disabling.
 */
export function useHostedVisibilityGuard({
  toolsetSlug,
  current,
  apply,
  isApplying,
}: {
  toolsetSlug: string | undefined;
  current: McpServerVisibility;
  apply: (next: McpServerVisibility) => void;
  isApplying: boolean;
}): {
  request: (next: McpServerVisibility) => void;
  publicUnavailable: boolean;
  dialogs: React.ReactNode;
} {
  const enabled = toolsetSlug !== undefined;
  const { data: toolset } = useToolset(toolsetSlug, {
    enabled,
    throwOnError: false,
  });
  const { data: environmentsData } = useListEnvironments(undefined, undefined, {
    enabled,
  });
  const { data: metadataData, isLoading: metadataLoading } = useGetMcpMetadata(
    { toolsetSlug: toolsetSlug ?? "" },
    undefined,
    // 404 means no attached environment, so it must not lock Public.
    { enabled, retry: false, throwOnError: false },
  );
  const environments = useMemo(
    () => environmentsData?.environments ?? [],
    [environmentsData?.environments],
  );
  const metadata = metadataData?.metadata;
  const attachedEnvironment = metadata?.defaultEnvironmentId
    ? (environments.find((e) => e.id === metadata.defaultEnvironmentId) ?? null)
    : null;
  const envVars = useEnvironmentVariables(toolset, environments, metadata);
  const systemVarNames = useMemo(
    () =>
      attachedEnvironment
        ? getSystemProvidedVariables(envVars, attachedEnvironment.slug)
        : [],
    [envVars, attachedEnvironment],
  );

  const [publicPending, setPublicPending] =
    useState<McpServerVisibility | null>(null);
  const [enablePending, setEnablePending] =
    useState<McpServerVisibility | null>(null);
  // Until the attached environment resolves, Public could skip the warning.
  const publicUnavailable =
    enabled && (metadataLoading || !environmentsData || !toolset);

  const needsEnableDialog = (next: McpServerVisibility) =>
    next === "disabled" || current === "disabled";

  const request = (next: McpServerVisibility) => {
    if (next === current) return;
    if (next === "public") {
      setPublicPending(next);
    } else if (needsEnableDialog(next)) {
      setEnablePending(next);
    } else {
      apply(next);
    }
  };

  const dialogs = toolsetSlug ? (
    <>
      <PublicMcpWarningDialog
        isOpen={publicPending !== null}
        onClose={() => setPublicPending(null)}
        onConfirm={() => {
          const next = publicPending;
          setPublicPending(null);
          if (!next) return;
          if (needsEnableDialog(next)) setEnablePending(next);
          else apply(next);
        }}
        isLoading={isApplying}
        toolsetSlug={toolsetSlug}
        environmentSlug={attachedEnvironment?.slug ?? ""}
        variableNames={systemVarNames}
      />
      <ServerEnableDialog
        isOpen={enablePending !== null}
        onClose={() => setEnablePending(null)}
        onConfirm={() => {
          if (enablePending) apply(enablePending);
          setEnablePending(null);
        }}
        isLoading={isApplying}
        currentlyEnabled={current !== "disabled"}
        targetIsPublic={enablePending === "public"}
      />
    </>
  ) : null;

  return { request, publicUnavailable, dialogs };
}
