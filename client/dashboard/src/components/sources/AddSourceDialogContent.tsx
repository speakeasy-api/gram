import { GettingStartedInstructions } from "@/components/functions/GettingStartedInstructions";
import ImportMCPTabContent from "@/components/sources/ImportMCPTabContent";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import DeployStep from "@/components/upload-asset/deploy-step";
import NameDeploymentStep from "@/components/upload-asset/name-deployment-step";
import UploadAssetStep from "@/components/upload-asset/step";
import UploadAssetStepper, {
  useStepper,
} from "@/components/upload-asset/stepper";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { useLatestDeployment, useListAssets } from "@gram/client/react-query";
import { Button, Dialog } from "@speakeasy-api/moonshine";
import { ArrowRightIcon, RefreshCcwIcon } from "lucide-react";
import React from "react";

interface AddSourceDialogContentProps {
  onCompletion?: () => void;
  initialTab?: "openapi" | "functions";
}

export default function AddSourceDialogContent({
  onCompletion,
  initialTab = "openapi",
}: AddSourceDialogContentProps) {
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;
  const isExternalMCPEnabled =
    telemetry.isFeatureEnabled("gram-external-mcp") ?? false;
  const [activeTab, setActiveTab] = React.useState<
    "openapi" | "functions" | "import-mcp"
  >(initialTab);

  return (
    <UploadAssetStepper.Provider step={1}>
      <Dialog.Header>
        <Dialog.Title>Add Source</Dialog.Title>
        <Dialog.Description>
          {isFunctionsEnabled || isExternalMCPEnabled
            ? "Upload an OpenAPI document, add Gram Functions, or import an MCP server"
            : "Upload an OpenAPI document to create tools"}
        </Dialog.Description>
      </Dialog.Header>
      {isFunctionsEnabled || isExternalMCPEnabled ? (
        <Tabs
          value={activeTab}
          onValueChange={(v) =>
            setActiveTab(v as "openapi" | "functions" | "import-mcp")
          }
          className="px-6"
        >
          <TabsList
            className={`grid w-full ${
              isFunctionsEnabled && isExternalMCPEnabled
                ? "grid-cols-3"
                : "grid-cols-2"
            }`}
          >
            <TabsTrigger value="openapi">OpenAPI</TabsTrigger>
            {isFunctionsEnabled && (
              <TabsTrigger value="functions">Functions</TabsTrigger>
            )}
            {isExternalMCPEnabled && (
              <TabsTrigger value="import-mcp">Import MCP</TabsTrigger>
            )}
          </TabsList>
          <TabsContent value="openapi" className="mt-0">
            <UploadAssetStepper.Frame className="p-8">
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
                  description="Gram will generate tools for your API."
                />
                <UploadAssetStep.Content>
                  <DeployStep />
                </UploadAssetStep.Content>
              </UploadAssetStep>
            </UploadAssetStepper.Frame>
          </TabsContent>
          {isFunctionsEnabled && (
            <TabsContent value="functions" className="mt-0">
              <GettingStartedInstructions />
            </TabsContent>
          )}
          {isExternalMCPEnabled && (
            <TabsContent value="import-mcp" className="mt-0">
              <ImportMCPTabContent onSuccess={onCompletion} />
            </TabsContent>
          )}
        </Tabs>
      ) : (
        <UploadAssetStepper.Frame className="p-8">
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
              description="Gram will generate tools for your API."
            />
            <UploadAssetStep.Content>
              <DeployStep />
            </UploadAssetStep.Content>
          </UploadAssetStep>
        </UploadAssetStepper.Frame>
      )}
      <Dialog.Footer>
        <FooterActions onCompletion={onCompletion} />
      </Dialog.Footer>
    </UploadAssetStepper.Provider>
  );
}

function FooterActions({ onCompletion }: { onCompletion?: () => void }) {
  const stepper = useStepper();
  const routes = useRoutes();

  const latestDeployment = useLatestDeployment(undefined, undefined, {
    enabled: false,
  });
  const assetsList = useListAssets(undefined, undefined, { enabled: false });

  const handleContinue = () => {
    assetsList.refetch();
    latestDeployment.refetch();
    routes.mcp.goTo();
    onCompletion?.();
  };

  const deploymentId = stepper.meta.current.deployment?.id;

  switch (stepper.state) {
    case "idle":
      return null;
    case "completed":
      return (
        <Button variant="primary" onClick={handleContinue}>
          Continue
          <ArrowRightIcon className="size-4" />
        </Button>
      );
    case "error":
      if (!deploymentId) {
        // This should never happen, but just in case
        return (
          <Button variant="primary" onClick={() => stepper.reset()}>
            <RefreshCcwIcon className="size-4" />
            Start Over
          </Button>
        );
      }

      return (
        <>
          <Button
            variant="primary"
            onClick={() => routes.deployments.deployment.goTo(deploymentId)}
          >
            View Logs
            <ArrowRightIcon className="size-4" />
          </Button>
        </>
      );
  }
}
