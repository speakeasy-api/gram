import { UploadAssetStepper } from "@/components/upload-asset";
import DeployStep from "@/components/upload-asset/deploy-step";
import NameDeploymentStep from "@/components/upload-asset/name-deployment-step";
import UploadAssetStep from "@/components/upload-asset/step";
import UploadFileStep from "@/components/upload-asset/upload-file-step";
import { useRoutes } from "@/routes";
import { useLatestDeployment, useListAssets } from "@gram/client/react-query";
import { Button, Dialog } from "@speakeasy-api/moonshine";
import { ArrowRightIcon, RefreshCcwIcon } from "lucide-react";
import React from "react";

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
      <Dialog open={open} onOpenChange={setOpen}>
        <Dialog.Content className="max-w-2xl!">
          <Dialog.Header>
            <Dialog.Title>New OpenAPI Source</Dialog.Title>
            <Dialog.Description>
              Upload a new OpenAPI document to use in addition to your existing
              documents.
            </Dialog.Description>
          </Dialog.Header>
          <UploadAssetStepper.Frame className="p-8 space-y-8">
            <UploadAssetStep.Root step={1}>
              <UploadAssetStep.Indicator />
              <UploadAssetStep.Header
                title="Upload OpenAPI Specification"
                description="Upload your OpenAPI specification to get started."
              />
              <UploadAssetStep.Content>
                <UploadFileStep />
              </UploadAssetStep.Content>
            </UploadAssetStep.Root>

            <UploadAssetStep.Root step={2}>
              <UploadAssetStep.Indicator />
              <UploadAssetStep.Header
                title="Name Your API"
                description="The tools generated will be scoped under this name."
              />
              <UploadAssetStep.Content>
                <NameDeploymentStep />
              </UploadAssetStep.Content>
            </UploadAssetStep.Root>

            <UploadAssetStep.Root step={3}>
              <UploadAssetStep.Indicator />
              <UploadAssetStep.Header
                title="Generate Tools"
                description="Gram will generate tools for your API."
              />
              <UploadAssetStep.Content>
                <DeployStep />
              </UploadAssetStep.Content>
            </UploadAssetStep.Root>
          </UploadAssetStepper.Frame>
          <Dialog.Footer>
            <FooterActions onClose={() => setOpen(false)} />
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </UploadAssetStepper.Provider>
  );
});
AddOpenAPIDialog.displayName = "AddOpenAPIDialog";

function FooterActions({ onClose }: { onClose?: () => void }) {
  const stepper = UploadAssetStepper.useContext();
  const routes = useRoutes();

  const latestDeployment = useLatestDeployment(undefined, undefined, {
    enabled: false,
  });
  const assetsList = useListAssets(undefined, undefined, { enabled: false });

  const handleClose = () => {
    onClose?.();
    stepper.reset();
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

export default AddOpenAPIDialog;
