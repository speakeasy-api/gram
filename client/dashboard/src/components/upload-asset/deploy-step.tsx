import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useListTools } from "@/hooks/toolTypes";
import { slugify } from "@/lib/constants";
import { useRoutes } from "@/routes";
import { Deployment } from "@gram/client/models/components";
import {
  useDeploymentLogs,
  useLatestDeployment,
} from "@gram/client/react-query";
import { Alert, Stack } from "@speakeasy-api/moonshine";
import React from "react";
import { Spinner } from "../ui/spinner";
import { Type } from "../ui/type";
import { useStep } from "./step";
import { useStepper } from "./stepper";

export default function DeployStep() {
  const stepper = useStepper();
  const step = useStep();
  const telemetry = useTelemetry();

  const toolsList = useListTools(
    { deploymentId: stepper.meta.current.deployment?.id },
    undefined,
    { enabled: step.state === "completed" },
  );

  const toolCount = React.useMemo(() => {
    const { deployment, uploadResult } = stepper.meta.current;
    if (!toolsList.data || !deployment || !uploadResult) return 0;

    const documentId = deployment!.openapiv3Assets.find(
      (doc) => doc.assetId === uploadResult?.asset.id,
    )?.id;

    return toolsList.data.tools.reduce((prev: number, cur) => {
      if (cur.type === "http" && cur.openapiv3DocumentId === documentId)
        return prev + 1;
      return prev;
    }, 0);
  }, [toolsList.data]);

  const deploymentLogs = useDeploymentLogs(
    {
      deploymentId: stepper.meta.current.deployment?.id ?? "",
    },
    undefined,
    { enabled: step.state === "completed" },
  );

  const deployHasErrors = React.useMemo(() => {
    if (!deploymentLogs.data) return false;
    return deploymentLogs.data.events.some(({ event }) =>
      event.includes("error"),
    );
  }, [deploymentLogs.data]);

  const createOrEvolveDeployment = useCreateDeployment();

  React.useEffect(() => {
    if (!step.isCurrentStep || step.state !== "idle") return;
    createOrEvolveDeployment().then((result) => {
      stepper.meta.current.deployment = result;

      if (result.status === "failed") {
        step.setState("failed");
        stepper.setState("error");
        telemetry.capture("onboarding_event", {
          action: "deployment_failed",
        });
      } else {
        step.setState("completed");
        stepper.setState("completed");
        telemetry.capture("onboarding_event", {
          action: "deployment_created",
          num_tools: result?.openapiv3ToolCount,
        });
      }

      if (result?.openapiv3ToolCount === 0) {
        telemetry.capture("onboarding_event", {
          action: "no_tools_found",
          error: "no_tools_found",
        });
      }
    });
  }, [step.isCurrentStep, step.state]);

  if (!step.isCurrentStep) return null;

  switch (step.state) {
    case "idle":
      return (
        <Stack direction="horizontal" gap={1} align="center">
          <Spinner />
          <Type>
            Gram is generating tools for your API. This may take a few seconds.
          </Type>
        </Stack>
      );
    case "completed":
      return (
        <DeployCompletedMessage
          toolCount={toolCount}
          hasErrors={deployHasErrors}
        />
      );
    case "failed":
      return <DeploymentFailedMessage />;
  }
}

function DeployCompletedMessage({
  toolCount,
  hasErrors,
}: {
  toolCount: number;
  hasErrors: boolean;
}) {
  const routes = useRoutes();
  const stepper = useStepper();

  const { deployment } = stepper.meta.current;

  if (!deployment) {
    throw new Error("Deployment not found");
  }

  if (!hasErrors)
    return (
      <Alert variant="success" dismissible={false} className="text-sm">
        The deployment succeeded! Gram generated {toolCount} tools for your API.
      </Alert>
    );

  return (
    <Alert variant="warning" dismissible={false} className="text-sm">
      The deployment succeeded with some errors. Check the{" "}
      <routes.deployments.deployment.Link
        params={[deployment?.id ?? ""]}
        className="text-link"
      >
        deployment logs
      </routes.deployments.deployment.Link>{" "}
      for more information.
    </Alert>
  );
}

function DeploymentFailedMessage() {
  const routes = useRoutes();
  const stepper = useStepper();

  const { deployment } = stepper.meta.current;

  if (!deployment) {
    return (
      <Alert variant="error" dismissible={false} className="text-sm">
        The deployment failed to be created. Please try again.
      </Alert>
    );
  }

  return (
    <Alert variant="error" dismissible={false} className="text-sm">
      The deployment failed. Check the{" "}
      <routes.deployments.deployment.Link
        params={[deployment?.id ?? ""]}
        className="text-link"
      >
        deployment logs
      </routes.deployments.deployment.Link>{" "}
      for more information.
    </Alert>
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

    const shouldCreateNew =
      !latestDeployment ||
      latestDeployment.data?.deployment?.openapiv3ToolCount === 0;

    let deployment: Deployment | undefined;
    if (shouldCreateNew) {
      const result = await client.deployments.create({
        idempotencyKey: crypto.randomUUID(),
        createDeploymentRequestBody: {
          openapiv3Assets: [
            {
              assetId: uploadResult.asset.id,
              name: assetName,
              slug: slugify(assetName),
            },
          ],
        },
      });
      deployment = result.deployment;
    } else {
      const result = await client.deployments.evolveDeployment({
        evolveForm: {
          upsertOpenapiv3Assets: [
            {
              assetId: uploadResult.asset.id,
              name: assetName,
              slug: slugify(assetName),
            },
          ],
        },
      });
      deployment = result.deployment;
    }

    if (!deployment) {
      throw new Error("Deployment not found");
    }

    // Wait for deployment to finish
    while (
      deployment.status !== "completed" &&
      deployment.status !== "failed"
    ) {
      await new Promise((resolve) => setTimeout(resolve, 100));
      deployment = await client.deployments.getById({
        id: deployment.id,
      });
    }

    return deployment;
  }, [latestDeployment.data, step.isCurrentStep]);

  return _do;
};
