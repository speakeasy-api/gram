import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { slugify } from "@/lib/constants";
import { Deployment } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import React from "react";
import { Type } from "../ui/type";
import Step from "./step";
import Stepper from "./stepper";

export default function NameDeploymentStep() {
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const stepper = Stepper.useContext();
  const step = Step.useContext();

  const latestDeployment = useLatestDeployment();
  const [value, setValue] = React.useState("");
  const [isPending, setIsPending] = React.useState(false);

  React.useEffect(() => {
    if (stepper.meta.current.file) {
      const name = stepper.meta.current.file.name.replace(/\.[^/.]+$/, "");
      setValue(slugify(name));
    }
  }, [step.isCurrentStep]);

  const validation = React.useMemo<string | null>(() => {
    if (!value) return "API name is required";

    if (value.length < 3) {
      return "API name must be at least 3 characters long";
    }

    if (!latestDeployment.data) return "The deployment name can't be validated";

    const isUnique = latestDeployment.data.deployment?.openapiv3Assets.every(
      (asset) => asset.slug !== value,
    );

    if (!isUnique) return "API name must be unique";

    return null;
  }, [value, latestDeployment.data]);

  const handleCreateDeployment = async () => {
    setIsPending(true);
    const asset = stepper.meta.current.uploadResult;

    if (!asset || !value) {
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
              assetId: asset.asset.id,
              name: value,
              slug: slugify(value),
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
              assetId: asset.asset.id,
              name: value,
              slug: slugify(value),
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

    stepper.meta.current.deployment = deployment;
    stepper.next();
    step.setState("completed");
    setIsPending(false);

    if (deployment.status === "failed") {
      telemetry.capture("onboarding_event", {
        action: "deployment_failed",
      });
    } else {
      telemetry.capture("onboarding_event", {
        action: "deployment_created",
        num_tools: deployment?.openapiv3ToolCount,
      });
    }

    if (deployment?.openapiv3ToolCount === 0) {
      telemetry.capture("onboarding_event", {
        action: "no_tools_found",
        error: "no_tools_found",
      });
    }
  };

  function handleValueChange(e: React.ChangeEvent<HTMLInputElement>) {
    setValue(e.target.value);
  }

  if (step.isCurrentStep && step.state === "idle") {
    return (
      <Stack gap={2}>
        <Stack
          direction={"horizontal"}
          gap={2}
          className="max-w-sm z-10 items-center"
        >
          <Input
            value={value}
            onChange={handleValueChange}
            placeholder="My API"
            disabled={isPending}
            className="h-9 py-0"
          />
          <Button
            variant="brand"
            onClick={() => handleCreateDeployment()}
            disabled={validation !== null || isPending}
          >
            CONTINUE
          </Button>
        </Stack>
        {validation !== null && (
          <span className="text-destructive">{validation}</span>
        )}
      </Stack>
    );
  } else if (step.state === "completed") {
    return <Type>✓ Source named "{value}"</Type>;
  } else if (!step.isCurrentStep) {
    return null;
  } else {
    return (
      <Type>
        An unexpected error occurred. Please refresh your browser, and try
        again.
      </Type>
    );
  }
}
