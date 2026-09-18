import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Button } from "@/components/ui/Button";
import { ensureToolsetWrapper } from "@/pages/mcp/gateway/ensureToolsetWrapper";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useListTools } from "@/hooks/toolTypes";
import { asTools } from "@/lib/toolTypes";
import { slugify } from "@/lib/constants";
import { useRoutes } from "@/routes";
import { Deployment } from "@gram/client/models/components/deployment.js";
import { DeploymentLogEvent } from "@gram/client/models/components/deploymentlogevent.js";
import { invalidateAllActiveDeployment } from "@gram/client/react-query/activeDeployment.js";
import { useDeploymentLogs } from "@gram/client/react-query/deploymentLogs.js";
import { invalidateAllLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { invalidateAllListAssets } from "@gram/client/react-query/listAssets.js";
import { invalidateAllListDeployments } from "@gram/client/react-query/listDeployments.js";
import { invalidateAllListTools } from "@gram/client/react-query/listTools.js";
import { invalidateAllListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useQueryClient } from "@tanstack/react-query";
import { Alert } from "@/components/ui/Alert";
import { Stack } from "@/components/ui/Stack";
import { ChevronDownIcon, ExternalLinkIcon } from "lucide-react";
import React from "react";
import { Spinner } from "@/components/ui/Spinner";
import { Text } from "@/components/ui/Text";
import { useStep } from "./step/use-step";
import { useStepper } from "./stepper/use-stepper";

export default function DeployStep({
  gateway,
  onPendingChange,
}: {
  onPendingChange?: (pending: boolean) => void;
  gateway?: {
    gatewayId: string | null;
    createdServerId: string | null;
    complete: (mcpServerId: string) => Promise<void>;
  };
}): React.JSX.Element | null {
  const stepper = useStepper();
  const step = useStep();
  const telemetry = useTelemetry();

  // Fetch tools when step is done (completed or failed) - deployment may fail due to
  // unrelated issues but tools for this asset may still have been created
  const stepDone = step.state === "completed" || step.state === "failed";
  const toolsList = useListTools(
    { deploymentId: stepper.meta.current.deployment?.id },
    undefined,
    { enabled: stepDone },
  );

  const client = useSdkClient();
  const mounted = React.useRef(false);
  React.useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const deploymentAttempted = React.useRef(false);
  const [terminalTools, setTerminalTools] = React.useState<ReturnType<
    typeof asTools
  > | null>(null);
  // Gateway creation must not consume cached or in-flight pre-terminal query data.
  const tools =
    gateway?.gatewayId && deploymentAttempted.current
      ? terminalTools
      : toolsList.data?.tools;
  const [deploymentPending, setDeploymentPending] = React.useState(false);
  const [deploymentError, setDeploymentError] = React.useState<string | null>(
    null,
  );
  const [deploymentRetry, setDeploymentRetry] = React.useState(0);
  const [creationPending, setCreationPending] = React.useState(false);
  React.useEffect(() => {
    onPendingChange?.(
      creationPending ||
        deploymentPending ||
        (step.isCurrentStep && step.state === "idle" && !deploymentError) ||
        (stepDone && !!toolsList.isLoading),
    );
  }, [
    creationPending,
    deploymentPending,
    step.isCurrentStep,
    step.state,
    stepDone,
    toolsList.isLoading,
    onPendingChange,
    deploymentError,
  ]);
  const toolsetCreationAttempted = React.useRef(false);
  const toolsSeeded = React.useRef(false);
  const wrapperAttempted = React.useRef(false);
  const createdWrapperId = React.useRef<string | null>(null);
  const [creationError, setCreationError] = React.useState<string | null>(null);
  const [retryAttempt, setRetryAttempt] = React.useState(0);

  const { toolCount, toolUrns } = React.useMemo(() => {
    const { deployment, uploadResult, assetName } = stepper.meta.current;
    if (!tools || !deployment || !uploadResult || !assetName) {
      return { toolCount: 0, toolUrns: [] as string[] };
    }

    const sourceSlug =
      stepper.meta.current.existingDocument?.slug ?? slugify(assetName);
    const documentId =
      deployment.openapiv3Assets.find((doc) => doc.slug === sourceSlug)?.id ??
      deployment.openapiv3Assets.find(
        (doc) => doc.assetId === uploadResult.asset.id,
      )?.id;

    const matchingTools = tools.filter(
      (tool) => tool.type === "http" && tool.openapiv3DocumentId === documentId,
    );

    return {
      toolCount: matchingTools.length,
      toolUrns: matchingTools.map((tool) => tool.toolUrn),
    };
  }, [tools, stepper.meta]);

  // Auto-create toolset after tools are loaded (regardless of overall deployment status)
  // The deployment may fail due to unrelated issues (e.g., external MCP), but if tools
  // were created for this asset, we should still create a toolset for them.
  React.useEffect(() => {
    if (gateway?.gatewayId && (deploymentError || deploymentPending)) return;
    // Only run when step processing is done (completed or failed) and we have tools
    const stepDone = step.state === "completed" || step.state === "failed";
    // A new version of an existing document already has servers carrying
    // its tools; minting another toolset for it would only add a duplicate.
    if (
      !stepDone ||
      toolsetCreationAttempted.current ||
      gateway?.createdServerId ||
      toolUrns.length === 0 ||
      (stepper.meta.current.existingDocument && !gateway?.gatewayId)
    ) {
      return;
    }

    const { assetName } = stepper.meta.current;
    if (!assetName) return;

    // Mark as attempted immediately to prevent duplicate calls
    toolsetCreationAttempted.current = true;

    setCreationPending(true);
    const createToolset = async () => {
      try {
        if (stepper.meta.current.existingDocument && gateway?.gatewayId) {
          // Updating a source does not identify a server: reuse only an
          // unambiguous existing association, never mint a duplicate toolset.
          const { toolsets } = await client.toolsets.list();
          if (!mounted.current) return;
          const matches = toolsets.filter((toolset) =>
            toolset.toolUrns?.some((urn) => toolUrns.includes(urn)),
          );
          if (matches.length !== 1 || !matches[0]) {
            throw new Error(
              "Source updated. Select the intended existing server from the gateway; its source association could not be determined uniquely.",
            );
          }
          const { mcpServers } = await client.mcpServers.list({
            toolsetId: matches[0].id,
          });
          if (!mounted.current) return;
          if (mcpServers.length !== 1 || !mcpServers[0]) {
            throw new Error(
              "Source updated. Select the intended existing server from the gateway; its source association could not be determined uniquely.",
            );
          }
          await gateway.complete(mcpServers[0].id);
          return;
        }
        // Gateway retries resume from the last successful stage.
        const toolset =
          (gateway?.gatewayId ? stepper.meta.current.toolset : null) ??
          (await client.toolsets.create({
            createToolsetRequestBody: {
              name: assetName,
              description: `Tools generated from ${assetName}`,
            },
          }));
        if (!mounted.current) return;
        if (gateway?.gatewayId) stepper.meta.current.toolset = toolset;

        if (!toolsSeeded.current) {
          await client.toolsets.updateBySlug({
            slug: toolset.slug,
            updateToolsetRequestBody: { toolUrns },
          });
          if (!mounted.current) return;
          toolsSeeded.current = true;
        }

        stepper.meta.current.toolset = toolset;
        if (gateway?.gatewayId) {
          if (!createdWrapperId.current) {
            const reconcile = wrapperAttempted.current;
            wrapperAttempted.current = true;
            createdWrapperId.current = await ensureToolsetWrapper(
              client,
              toolset,
              reconcile,
            );
          }
          if (!mounted.current) return;
          await gateway.complete(createdWrapperId.current);
        }
        if (!mounted.current) return;
        telemetry.capture("onboarding_event", {
          action: "toolset_auto_created",
          toolset_name: assetName,
          tool_count: toolUrns.length,
        });
      } catch (error) {
        if (!mounted.current) return;
        if (gateway?.gatewayId) {
          setCreationError(
            !stepper.meta.current.toolset &&
              !stepper.meta.current.existingDocument
              ? "Toolset creation could not be confirmed. Do not retry creation. Return to the gateway and manually inspect toolsets to recover any created resource before starting again."
              : error instanceof Error
                ? error.message
                : "Failed to create MCP server",
          );
        } else {
          // Standalone toolset creation remains optional.
          console.error("Failed to auto-create toolset:", error);
        }
      } finally {
        if (mounted.current) setCreationPending(false);
      }
    };

    void createToolset();
  }, [
    step.state,
    toolUrns,
    client,
    stepper.meta,
    telemetry,
    gateway,
    retryAttempt,
    deploymentError,
    deploymentPending,
  ]);

  const deploymentLogs = useDeploymentLogs(
    {
      deploymentId: stepper.meta.current.deployment?.id ?? "",
    },
    undefined,
    { enabled: stepDone },
  );

  const createOrEvolveDeployment = useCreateDeployment(mounted);

  React.useEffect(() => {
    if (
      !step.isCurrentStep ||
      (step.state !== "idle" && !deploymentRetry) ||
      deploymentAttempted.current ||
      gateway?.createdServerId
    )
      return;
    deploymentAttempted.current = true;
    setDeploymentPending(true);
    createOrEvolveDeployment()
      .then(async (result) => {
        if (!mounted.current) return;
        if (gateway?.gatewayId) {
          const response = await client.tools.list({ deploymentId: result.id });
          if (!mounted.current) return;
          setTerminalTools(asTools(response.tools));
        }
        stepper.meta.current.deployment = result;

        // Always mark as "completed" so we can check tool count
        // The actual success/failure is determined by whether tools were created for THIS source
        step.setState("completed");
        stepper.setState("completed");

        telemetry.capture("onboarding_event", {
          action:
            result.status === "failed"
              ? "deployment_failed"
              : "deployment_created",
          num_tools: result?.openapiv3ToolCount,
          deployment_status: result.status,
        });

        if (result?.openapiv3ToolCount === 0) {
          telemetry.capture("onboarding_event", {
            action: "no_tools_found",
            error: "no_tools_found",
          });
        }
      })
      .catch((err) => {
        if (!mounted.current) return;
        setDeploymentError(
          err instanceof Error ? err.message : "Deployment failed",
        );
        console.error("Deployment failed:", err);
        step.setState("failed");
        stepper.setState("error");
      })
      .finally(() => {
        if (mounted.current) setDeploymentPending(false);
      });
  }, [
    step.isCurrentStep,
    step.state,
    createOrEvolveDeployment,
    deploymentRetry,
    gateway?.createdServerId,
    gateway?.gatewayId,
    client.tools,
    step,
    stepper,
    telemetry,
  ]);

  if (gateway?.gatewayId && deploymentError) {
    return (
      <Stack gap={3}>
        <Alert variant="error" dismissible={false}>
          {deploymentError}
        </Alert>
        <Button
          onClick={() => {
            setDeploymentError(null);
            setDeploymentPending(true);
            deploymentAttempted.current = false;
            setDeploymentRetry((attempt) => attempt + 1);
          }}
        >
          Retry deployment
        </Button>
      </Stack>
    );
  }

  if (gateway?.gatewayId && creationError) {
    return (
      <Stack gap={3}>
        <Alert variant="error" dismissible={false}>
          {creationError}
        </Alert>
        {stepper.meta.current.toolset && (
          <Button
            onClick={() => {
              setCreationError(null);
              toolsetCreationAttempted.current = false;
              setRetryAttempt((attempt) => attempt + 1);
            }}
          >
            Retry
          </Button>
        )}
      </Stack>
    );
  }

  if (!step.isCurrentStep) return null;

  if (step.state === "idle") {
    return (
      <Stack direction="horizontal" gap={1} align="center">
        <Spinner />
        <Text>
          The platform is generating tools for your API. This may take a few
          seconds.
        </Text>
      </Stack>
    );
  }

  // Step is done - determine message based on whether THIS source's tools were created
  return (
    <DeployCompletedMessage
      toolCount={toolCount}
      toolsLoading={toolsList.isLoading}
      deploymentLogs={deploymentLogs.data?.events}
    />
  );
}

function DeployCompletedMessage({
  toolCount,
  toolsLoading,
  deploymentLogs,
}: {
  toolCount: number;
  toolsLoading: boolean;
  deploymentLogs?: DeploymentLogEvent[];
}) {
  const stepper = useStepper();
  const [logsOpen, setLogsOpen] = React.useState(false);

  const { deployment } = stepper.meta.current;

  // Still loading tools
  if (toolsLoading) {
    return (
      <Stack direction="horizontal" gap={1} align="center">
        <Spinner />
        <Text>Checking generated tools...</Text>
      </Stack>
    );
  }

  // Tools were created for this source - success!
  if (toolCount > 0) {
    if (!deployment) return null;
    return (
      <DeploymentDetailsCollapsible
        deployment={deployment}
        logs={deploymentLogs ?? []}
        open={logsOpen}
        onOpenChange={setLogsOpen}
      />
    );
  }

  // No tools were created for this source
  return (
    <Stack gap={3}>
      <Alert variant="error" dismissible={false} className="text-sm">
        No tools were generated from your API.
      </Alert>
      {deployment && deploymentLogs && deploymentLogs.length > 0 && (
        <DeploymentDetailsCollapsible
          deployment={deployment}
          logs={deploymentLogs}
          open={logsOpen}
          onOpenChange={setLogsOpen}
        />
      )}
    </Stack>
  );
}

function DeploymentDetailsCollapsible({
  deployment,
  logs,
  open,
  onOpenChange,
}: {
  deployment: Deployment;
  logs: DeploymentLogEvent[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const routes = useRoutes();

  return (
    <Collapsible open={open} onOpenChange={onOpenChange}>
      <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors">
        <ChevronDownIcon
          className={`h-4 w-4 transition-transform ${open ? "rotate-180" : ""}`}
        />
        Deployment details
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2">
        <div className="bg-muted/30 space-y-2 border p-3">
          <div className="max-h-60 space-y-1 overflow-y-scroll font-mono text-xs">
            {logs.map((log) => (
              <div
                key={log.id}
                className={`${log.event.includes("error") ? "text-destructive" : "text-muted-foreground"}`}
              >
                {log.message}
              </div>
            ))}
          </div>
          <div className="border-t pt-2">
            <routes.deployments.deployment.Link
              params={[deployment.id]}
              className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs"
            >
              View full deployment details
              <ExternalLinkIcon className="h-3 w-3" />
            </routes.deployments.deployment.Link>
          </div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

/**
 * Returns a function that creates or evolves a deployment based on the latest
 * deployment state.
 */
const useCreateDeployment = (
  mounted: React.RefObject<boolean>,
): (() => Promise<Deployment>) => {
  const stepper = useStepper();
  const client = useSdkClient();
  const queryClient = useQueryClient();

  const _do = React.useCallback(async () => {
    const { uploadResult, assetName, existingDocument } = stepper.meta.current;

    if (!uploadResult || !assetName) {
      throw new Error("Asset or file not found");
    }

    let deployment = stepper.meta.current.deployment;
    if (!deployment) {
      const result = await client.deployments.evolveDeployment({
        evolveForm: {
          nonBlocking: true,
          upsertOpenapiv3Assets: [
            {
              assetId: uploadResult.asset.id,
              name: existingDocument?.name ?? assetName,
              slug: existingDocument?.slug ?? slugify(assetName),
            },
          ],
        },
      });

      if (!mounted.current) throw new Error("Deployment view closed");
      deployment = result.deployment ?? null;
      if (!deployment) {
        throw new Error("Deployment not found");
      }

      stepper.meta.current.deployment = deployment;
    }

    // Poll until the deployment reaches a terminal state so we can
    // report accurate tool counts back to the stepper UI.
    const maxAttempts = 600; // 5 minutes at 500ms intervals
    let attempts = 0;
    while (
      deployment.status !== "completed" &&
      deployment.status !== "failed"
    ) {
      if (++attempts >= maxAttempts) {
        throw new Error("Deployment timed out waiting for completion");
      }
      await new Promise((resolve) => {
        void setTimeout(resolve, 500);
      });
      if (!mounted.current) throw new Error("Deployment view closed");
      deployment = (await client.deployments.getById({
        id: deployment.id,
      })) as Deployment;
      if (!mounted.current) throw new Error("Deployment view closed");
      stepper.meta.current.deployment = deployment;
    }

    // The sources shelf and every source page read these from the cache;
    // without this they keep showing the version just replaced.
    await Promise.all([
      invalidateAllActiveDeployment(queryClient),
      invalidateAllLatestDeployment(queryClient),
      invalidateAllListDeployments(queryClient),
      invalidateAllListAssets(queryClient),
      invalidateAllListTools(queryClient),
      // MCP usage on the shelf and source pages is read from the toolsets.
      invalidateAllListToolsets(queryClient),
    ]);

    return deployment;
  }, [client.deployments, mounted, queryClient, stepper.meta]);

  return _do;
};
