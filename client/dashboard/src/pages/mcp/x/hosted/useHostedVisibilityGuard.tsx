import { PublicMcpWarningDialog } from "@/components/public-mcp-warning-dialog";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { useToolset } from "@/hooks/toolTypes";
import { getSystemProvidedVariables } from "@/pages/mcp/environmentVariableUtils";
import { useEnvironmentVariables } from "@/pages/mcp/useEnvironmentVariables";
import { isCanonicalHostedWrapper } from "@/pages/mcp/x/MCPServerDetailsRouting";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import React, { useMemo, useState } from "react";

// Confirmations before a hosted server's visibility changes, as the toolset page had.
export function useHostedVisibilityGuard({
  server,
  hosted,
  apply,
  isApplying,
}: {
  server: McpServer;
  hosted: boolean;
  apply: (next: McpServerVisibility) => void;
  isApplying: boolean;
}): {
  request: (next: McpServerVisibility) => void;
  publicUnavailable: boolean;
  dialogs: React.ReactNode;
} {
  const toolsetSlug = hosted ? server.toolset?.slug : undefined;
  const canonical = hosted && isCanonicalHostedWrapper(server);
  const enabled = toolsetSlug !== undefined;
  const { data: toolset } = useToolset(toolsetSlug, {
    enabled,
    throwOnError: false,
  });
  const { data: environmentsData } = useListEnvironments(undefined, undefined, {
    enabled,
  });
  // A canonical server shares its toolset's metadata; any other host has its own.
  const {
    data: metadataData,
    isLoading: metadataLoading,
    error: metadataError,
  } = useGetMcpMetadata(
    canonical ? { toolsetSlug: toolsetSlug ?? "" } : { mcpServerId: server.id },
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

  const current = server.visibility;
  // Keeps the visibility the warning opened from, so a refetch can't change the next dialog.
  const [publicPending, setPublicPending] = useState<{
    next: McpServerVisibility;
    from: McpServerVisibility;
  } | null>(null);
  const [enablePending, setEnablePending] =
    useState<McpServerVisibility | null>(null);
  const [oauthBlockOpen, setOauthBlockOpen] = useState(false);
  // Until the attached environment resolves, Public could skip the warning.
  const publicUnavailable =
    hosted &&
    (!enabled ||
      metadataLoading ||
      (!!metadataError &&
        !(
          metadataError instanceof GramError && metadataError.statusCode === 404
        )) ||
      !environmentsData ||
      !toolset);

  const needsEnableDialog = (next: McpServerVisibility) =>
    next === "disabled" || current === "disabled";
  // The server refuses to flip public access while toolset OAuth is attached.
  const blockedByOAuth = (next: McpServerVisibility) =>
    canonical &&
    !!toolset?.externalOauthServer &&
    next !== "disabled" &&
    (next === "public") !== (toolset.mcpIsPublic ?? false);

  const request = (next: McpServerVisibility) => {
    if (next === current) return;
    if (blockedByOAuth(next)) {
      setOauthBlockOpen(true);
    } else if (next === "public") {
      setPublicPending({ next, from: current });
    } else if (needsEnableDialog(next)) {
      setEnablePending(next);
    } else {
      apply(next);
    }
  };

  const dialogs = hosted ? (
    <>
      <Dialog open={oauthBlockOpen} onOpenChange={setOauthBlockOpen}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Remove external OAuth first</Dialog.Title>
            <Dialog.Description>
              This server has an external OAuth configuration. Remove it or
              convert it to a session issuer in Settings before changing who can
              reach the server.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button onClick={() => setOauthBlockOpen(false)}>Close</Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
      {toolsetSlug && (
        <PublicMcpWarningDialog
          isOpen={publicPending !== null}
          onClose={() => setPublicPending(null)}
          onConfirm={() => {
            const pending = publicPending;
            setPublicPending(null);
            if (!pending) return;
            if (pending.from === "disabled") setEnablePending(pending.next);
            else apply(pending.next);
          }}
          isLoading={isApplying}
          toolsetSlug={toolsetSlug}
          environmentSlug={attachedEnvironment?.slug ?? ""}
          variableNames={systemVarNames}
        />
      )}
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
