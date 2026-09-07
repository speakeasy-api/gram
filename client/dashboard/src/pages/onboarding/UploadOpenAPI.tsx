import { FormPage } from "@/components/page-templates";
import { Stack } from "@/components/ui/Stack";

import { Text } from "@/components/ui/Text";
import DeployStep from "@/components/upload-asset/deploy-step";
import NameDeploymentStep from "@/components/upload-asset/name-deployment-step";
import UploadAssetStep from "@/components/upload-asset/step";
import UploadAssetStepper from "@/components/upload-asset/stepper";
import { useStepper } from "@/components/upload-asset/stepper/use-stepper";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { useRoutes } from "@/routes";
import { Button } from "@/components/ui/Button";
import { useProject } from "@/contexts/Auth";
import { ArrowRightIcon, RefreshCcwIcon } from "lucide-react";

export default function UploadOpenAPI(): JSX.Element {
  const project = useProject();
  return (
    <FormPage
      scope="project:write"
      resourceId={project.id}
      title="Import OpenAPI specification"
      description="Upload your OpenAPI spec to automatically generate tools for every endpoint. Supports JSON and YAML formats."
    >
      <div>
        {/* Stepper */}
        <UploadAssetStepper.Provider step={1}>
          <UploadAssetStepper.Frame>
            <UploadAssetStep step={1}>
              <UploadAssetStep.Indicator />
              <UploadAssetStep.Header
                title="Upload OpenAPI Specification"
                description="Upload your OpenAPI specification to get started."
              />
              <UploadAssetStep.Content>
                <UploadFileStep />
              </UploadAssetStep.Content>
            </UploadAssetStep>

            <UploadAssetStep step={2}>
              <UploadAssetStep.Indicator />
              <UploadAssetStep.Header
                title="Name Your API"
                description="The tools generated will be scoped under this name."
              />
              <UploadAssetStep.Content>
                <NameDeploymentStep />
              </UploadAssetStep.Content>
            </UploadAssetStep>

            <UploadAssetStep step={3}>
              <UploadAssetStep.Indicator />
              <UploadAssetStep.Header
                title="Generate Tools"
                description="The platform will generate tools for your API."
              />
              <UploadAssetStep.Content>
                <DeployStep />
              </UploadAssetStep.Content>
            </UploadAssetStep>

            <Stack direction="horizontal" justify="start">
              <FooterActions />
            </Stack>
          </UploadAssetStepper.Frame>
        </UploadAssetStepper.Provider>

        {/* Help text */}
        <Text small muted className="mt-6">
          Don't have an OpenAPI spec?{" "}
          <a
            href="https://www.speakeasy.com/docs/gram"
            target="_blank"
            rel="noopener noreferrer"
            className="text-primary hover:underline"
          >
            Learn how to create one
          </a>{" "}
          or try our sample specs.
        </Text>
      </div>
    </FormPage>
  );
}

function FooterActions() {
  const stepper = useStepper();
  const routes = useRoutes();

  const deploymentId = stepper.meta.current.deployment?.id;

  switch (stepper.state) {
    case "idle":
      return null;
    case "completed":
      return (
        <Button variant="primary" onClick={() => routes.mcp.goTo()}>
          <Button.Text>Continue</Button.Text>
          <Button.RightIcon>
            <ArrowRightIcon className="size-4" />
          </Button.RightIcon>
        </Button>
      );
    case "error":
      if (!deploymentId) {
        // This should never happen, but just in case
        return (
          <Button variant="primary" onClick={stepper.reset}>
            <Button.LeftIcon>
              <RefreshCcwIcon className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Try Again</Button.Text>
          </Button>
        );
      }

      return (
        <>
          <Button
            variant="primary"
            onClick={() => routes.deployments.deployment.goTo(deploymentId)}
          >
            <Button.Text>View Logs</Button.Text>
            <Button.RightIcon>
              <ArrowRightIcon className="size-4" />
            </Button.RightIcon>
          </Button>
        </>
      );
  }
}
