import { slugify } from "@/lib/constants";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Stack } from "@/components/ui/Stack";
import React from "react";
import { Text } from "@/components/ui/Text";
import { useStep } from "./step/use-step";
import { useStepper } from "./stepper/use-stepper";

export default function NameDeploymentStep(): React.JSX.Element | null {
  const stepper = useStepper();
  const step = useStep();

  const latestDeployment = useLatestDeployment();

  const [value, setValue] = React.useState("");

  React.useEffect(() => {
    if (stepper.meta.current.file) {
      const name = stepper.meta.current.file.name.replace(/\.[^/.]+$/, "");
      setValue(slugify(name));
    }
  }, [step.isCurrentStep, stepper.meta]);

  const validation = React.useMemo<string | null>(() => {
    if (!value) return "API name is required";

    if (value.length < 3) {
      return "API name must be at least 3 characters long";
    }

    if (!latestDeployment.data) return "The deployment name can't be validated";

    // If there's no deployment or no assets, the name is unique by default
    const existingAssets =
      latestDeployment.data.deployment?.openapiv3Assets ?? [];
    const isUnique = existingAssets.every((asset) => asset.slug !== value);

    if (!isUnique) return "API name must be unique";

    return null;
  }, [value, latestDeployment.data]);

  function handleNameAsset() {
    stepper.meta.current.assetName = value;
    step.setState("completed");
    stepper.next();
  }

  function handleValueChange(next: string) {
    setValue(next);
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
            className="z-10 max-w-sm items-center"
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
    return <Text>✓ Source named "{stepper.meta.current.assetName}"</Text>;
  } else if (!step.isCurrentStep) {
    return null;
  } else {
    return (
      <Text>
        An unexpected error occurred. Please refresh your browser, and try
        again.
      </Text>
    );
  }
}
