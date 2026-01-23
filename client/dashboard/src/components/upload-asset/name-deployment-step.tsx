import { slugify } from "@/lib/constants";
import { useLatestDeployment } from "@gram/client/react-query";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import React from "react";
import { Type } from "../ui/type";
import { useStep } from "./step";
import { useStepper } from "./stepper";

export default function NameDeploymentStep() {
  const stepper = useStepper();
  const step = useStep();

  const latestDeployment = useLatestDeployment();

  const [value, setValue] = React.useState("");

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

    // If there's no deployment or no assets, the name is unique by default
    const existingAssets = latestDeployment.data.deployment?.openapiv3Assets ?? [];
    const isUnique = existingAssets.every((asset) => asset.slug !== value);

    if (!isUnique) return "API name must be unique";

    return null;
  }, [value, latestDeployment.data]);

  function handleNameAsset() {
    stepper.meta.current.assetName = value;
    step.setState("completed");
    stepper.next();
  }

  function handleValueChange(e: React.ChangeEvent<HTMLInputElement>) {
    setValue(e.target.value);
  }

  if (step.isCurrentStep && step.state === "idle") {
    return (
      <form
        onSubmit={(e) => {
          e.preventDefault();
          handleNameAsset();
        }}
      >
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
              className="h-9 py-0"
              autoFocus
            />
            <Button
              type="submit"
              variant="brand"
              disabled={validation !== null}
            >
              CONTINUE
            </Button>
          </Stack>
          {validation !== null && (
            <span className="text-destructive">{validation}</span>
          )}
        </Stack>
      </form>
    );
  } else if (step.state === "completed") {
    return <Type>✓ Source named "{stepper.meta.current.assetName}"</Type>;
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
