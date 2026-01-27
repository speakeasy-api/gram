import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useListTools } from "@/hooks/toolTypes";
import { slugify } from "@/lib/constants";
import { useRoutes } from "@/routes";
import { Deployment, DeploymentLogEvent } from "@gram/client/models/components";
import {
  useDeploymentLogs,
  useLatestDeployment,
} from "@gram/client/react-query";
import { Alert, Stack } from "@speakeasy-api/moonshine";
import { ChevronDownIcon, ExternalLinkIcon } from "lucide-react";
import React from "react";
import { Spinner } from "../ui/spinner";
import { Type } from "../ui/type";
import { useStep } from "./step";
import { useStepper } from "./stepper";

export default function DeployStep() {
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
  const toolsetCreationAttempted = React.useRef(false);

  const { toolCount, toolUrns } = React.useMemo(() => {
    const { deployment, uploadResult } = stepper.meta.current;
    if (!toolsList.data || !deployment || !uploadResult) {
      return { toolCount: 0, toolUrns: [] as string[] };
    }

    const documentId = deployment!.openapiv3Assets.find(
      (doc) => doc.assetId === uploadResult?.asset.id,
    )?.id;

    const matchingTools = toolsList.data.tools.filter(
      (tool) => tool.type === "http" && tool.openapiv3DocumentId === documentId,
    );

    return {
      toolCount: matchingTools.length,
      toolUrns: matchingTools.map((tool) => tool.toolUrn),
    };
  }, [toolsList.data]);

  // Auto-create toolset after tools are loaded (regardless of overall deployment status)
  // The deployment may fail due to unrelated issues (e.g., external MCP), but if tools
  // were created for this asset, we should still create a toolset for them.
  React.useEffect(() => {
    // Only run when step processing is done (completed or failed) and we have tools
    const stepDone = step.state === "completed" || step.state === "failed";
    if (
      !stepDone ||
      toolsetCreationAttempted.current ||
      toolUrns.length === 0
    ) {
      return;
    }

    const { assetName } = stepper.meta.current;
    if (!assetName) return;

    // Mark as attempted immediately to prevent duplicate calls
    toolsetCreationAttempted.current = true;

    const createToolset = async () => {
      try {
        // First create the toolset without tools
        const toolset = await client.toolsets.create({
          createToolsetRequestBody: {
            name: assetName,
            description: `Tools generated from ${assetName}`,
          },
        });

        // Then add the tools via update
        await client.toolsets.updateBySlug({
          slug: toolset.slug,
          updateToolsetRequestBody: {
            toolUrns,
          },
        });

        stepper.meta.current.toolset = toolset;
        telemetry.capture("onboarding_event", {
          action: "toolset_auto_created",
          toolset_name: assetName,
          tool_count: toolUrns.length,
        });
      } catch (error) {
        // Silently fail - toolset creation is optional
        console.error("Failed to auto-create toolset:", error);
      }
    };

    createToolset();
  }, [step.state, toolUrns.length]);

  const deploymentLogs = useDeploymentLogs(
    {
      deploymentId: stepper.meta.current.deployment?.id ?? "",
    },
    undefined,
    { enabled: stepDone },
  );

  const createOrEvolveDeployment = useCreateDeployment();

  React.useEffect(() => {
    if (!step.isCurrentStep || step.state !== "idle") return;
    createOrEvolveDeployment().then((result) => {
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
    });
  }, [step.isCurrentStep, step.state]);

  if (!step.isCurrentStep) return null;

  if (step.state === "idle") {
    return (
      <Stack direction="horizontal" gap={1} align="center">
        <Spinner />
        <Type>
          Gram is generating tools for your API. This may take a few seconds.
        </Type>
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
        <Type>Checking generated tools...</Type>
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
      <CollapsibleTrigger className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors">
        <ChevronDownIcon
          className={`h-4 w-4 transition-transform ${open ? "rotate-180" : ""}`}
        />
        Deployment details
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2">
        <div className="rounded-md border bg-muted/30 p-3 space-y-2">
          <div className="font-mono text-xs max-h-60 overflow-y-scroll space-y-1">
            {logs.map((log) => (
              <div
                key={log.id}
                className={`${log.event.includes("error") ? "text-destructive" : "text-muted-foreground"}`}
              >
                {log.message}
              </div>
            ))}
          </div>
          <div className="pt-2 border-t">
            <routes.deployments.deployment.Link
              params={[deployment.id]}
              className="text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
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
const useCreateDeployment = (): (() => Promise<Deployment>) => {
  const latestDeployment = useLatestDeployment();
  const stepper = useStepper();
  const step = useStep();
  const client = useSdkClient();

  const _do = React.useCallback(async () => {
    const { uploadResult, assetName } = stepper.meta.current;

    if (!uploadResult || !assetName) {
      throw new Error("Asset or file not found");
    }

    const deployment = await client.deployments
      .evolveDeployment({
        evolveForm: {
          upsertOpenapiv3Assets: [
            {
              assetId: uploadResult.asset.id,
              name: assetName,
              slug: slugify(assetName),
            },
          ],
        },
      })
      .then((result) => result.deployment);

    if (!deployment) {
      throw new Error("Deployment not found");
    }

    return deployment;
  }, [latestDeployment.data, step.isCurrentStep]);

  return _do;
};
