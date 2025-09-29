import { UploadAssetStepper, useStepper } from "@/components/upload-asset";
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

  // Reset the stepper when the dialog is closed and the close animation ends
  React.useEffect(() => {
    const cleanup = () => {
      if (!open) stepper.reset();
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
          <Dialog.Title>New OpenAPI Source</Dialog.Title>
          <Dialog.Description>
            Upload a new OpenAPI document to use in addition to your existing
            documents.
          </Dialog.Description>
        </Dialog.Header>
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

export default AddOpenAPIDialog;
