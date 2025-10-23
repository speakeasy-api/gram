import UploadAssetStepper, {
  useStepper,
} from "@/components/upload-asset/stepper";
import DeployStep from "@/components/upload-asset/deploy-step";
import NameDeploymentStep from "@/components/upload-asset/name-deployment-step";
import UploadAssetStep from "@/components/upload-asset/step";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { useRoutes } from "@/routes";
import { useLatestDeployment, useListAssets } from "@gram/client/react-query";
import { Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { ArrowRightIcon, RefreshCcwIcon, Check, Copy } from "lucide-react";
import React from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useTelemetry } from "@/contexts/Telemetry";
import { Type } from "@/components/ui/type";

export interface AddOpenAPIDialogRef {
  setOpen: (open: boolean) => void;
}

const AddOpenAPIDialog = React.forwardRef<AddOpenAPIDialogRef>((_, ref) => {
  const [open, setOpen] = React.useState(false);

  React.useImperativeHandle(ref, () => ({
    setOpen,
  }));

  return (
    <UploadAssetStepper.Provider step={1}>
      <AddOpenAPIDialogInternals open={open} onOpenChange={setOpen} />
    </UploadAssetStepper.Provider>
  );
});

const AddOpenAPIDialogInternals = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const stepper = useStepper();
  const dialogRef = React.useRef<HTMLDivElement>(null);
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;
  const [activeTab, setActiveTab] = React.useState<"openapi" | "functions">(
    "openapi",
  );

  // Reset the stepper when the dialog is closed and the close animation ends
  React.useEffect(() => {
    const cleanup = () => {
      if (!open) {
        stepper.reset();
        setActiveTab("openapi");
      }
    };

    dialogRef.current?.addEventListener("animationend", cleanup);

    return () => {
      dialogRef.current?.removeEventListener("animationend", cleanup);
    };
  }, [open]);

  function handleOpenChange(value: boolean) {
    onOpenChange(value);
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content ref={dialogRef} className="max-w-2xl!">
        <Dialog.Header>
          <Dialog.Title>Add Source</Dialog.Title>
          <Dialog.Description>
            {isFunctionsEnabled
              ? "Upload an OpenAPI document or add Gram Functions to create tools"
              : "Upload an OpenAPI document to create tools"}
          </Dialog.Description>
        </Dialog.Header>
        {isFunctionsEnabled ? (
          <Tabs
            value={activeTab}
            onValueChange={(v) => setActiveTab(v as "openapi" | "functions")}
            className="px-6"
          >
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="openapi">OpenAPI</TabsTrigger>
              <TabsTrigger value="functions">Functions</TabsTrigger>
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
            <TabsContent value="functions" className="mt-0">
              <FunctionsInstructions />
            </TabsContent>
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
          <FooterActions onClose={() => handleOpenChange(false)} />
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
};

function FooterActions({ onClose }: { onClose?: () => void }) {
  const stepper = useStepper();
  const routes = useRoutes();

  const latestDeployment = useLatestDeployment(undefined, undefined, {
    enabled: false,
  });
  const assetsList = useListAssets(undefined, undefined, { enabled: false });

  const handleClose = () => {
    onClose?.();
  };

  const handleContinue = () => {
    handleClose();
    assetsList.refetch();
    latestDeployment.refetch();
    routes.toolsets.goTo();
  };

  const deploymentId = stepper.meta.current.deployment?.id;

  switch (stepper.state) {
    case "idle":
      return (
        <Button variant="tertiary" onClick={handleClose}>
          Back
        </Button>
      );
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

function FunctionsInstructions() {
  const [copiedIndex, setCopiedIndex] = React.useState<number | null>(null);

  const commands = [
    {
      label: "Install the Gram CLI",
      command: "npm install -g @gram-ai/cli",
    },
    {
      label: "Authenticate with Gram",
      command: "gram auth",
    },
    {
      label: "Build your functions",
      command: "npm run build",
    },
    {
      label: "Upload to Gram",
      command:
        'gram upload --type function --location dist/functions.zip --name "My Functions" --slug my-functions',
    },
  ];

  const handleCopy = (command: string, index: number) => {
    navigator.clipboard.writeText(command);
    setCopiedIndex(index);
    setTimeout(() => setCopiedIndex(null), 2000);
  };

  return (
    <div className="p-8">
      <Stack gap={4}>
        {commands.map((item, index) => (
          <Stack key={index} gap={2}>
            <Type small className="font-medium">
              {index + 1}. {item.label}
            </Type>
            <div className="relative group">
              <pre className="p-4 rounded-md font-mono text-sm overflow-x-auto border">
                {item.command}
              </pre>
              <Button
                variant="tertiary"
                size="sm"
                onClick={() => handleCopy(item.command, index)}
                className="absolute top-2 right-2"
              >
                {copiedIndex === index ? (
                  <Check className="w-4 h-4" />
                ) : (
                  <Copy className="w-4 h-4" />
                )}
              </Button>
            </div>
          </Stack>
        ))}
      </Stack>
    </div>
  );
}

export default AddOpenAPIDialog;
