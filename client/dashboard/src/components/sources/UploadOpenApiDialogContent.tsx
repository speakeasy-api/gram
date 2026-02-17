import { OpenApiSourceInput } from "@/components/OpenApiSourceInput";
import { Spinner } from "@/components/ui/spinner";
import { useUploadOpenAPISteps } from "@/pages/onboarding/UploadOpenAPI";
import { UploadedDocument } from "@/pages/onboarding/Wizard";
import { Button, Dialog } from "@speakeasy-api/moonshine";
import React from "react";
import { toast } from "sonner";

interface UploadOpenApiDialogContentProps {
  documentSlug: string;
  onClose: () => void;
  onSuccess: () => void;
}

export function UploadOpenApiDialogContent({
  documentSlug,
  onClose,
  onSuccess,
}: UploadOpenApiDialogContentProps) {
  const {
    handleSpecUpload,
    handleUrlUpload,
    createDeployment,
    file,
    undoSpecUpload,
  } = useUploadOpenAPISteps();
  const [isDeploying, setIsDeploying] = React.useState(false);

  const deploySpecUpdate = async () => {
    setIsDeploying(true);
    try {
      await createDeployment(documentSlug);
      toast.success("OpenAPI document deployed");
      onSuccess();
    } catch (error) {
      toast.error("Failed to deploy OpenAPI document");
      console.error("Failed to deploy:", error);
    } finally {
      setIsDeploying(false);
    }
  };

  const handleClose = () => {
    undoSpecUpload();
    onClose();
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>New OpenAPI Version</Dialog.Title>
        <Dialog.Description>
          You are creating a new version of document {documentSlug}
        </Dialog.Description>
      </Dialog.Header>
      {!file ? (
        <OpenApiSourceInput
          onUpload={handleSpecUpload}
          onUrlUpload={handleUrlUpload}
          documentSlug={documentSlug}
        />
      ) : (
        <UploadedDocument
          file={file}
          onReset={undoSpecUpload}
          defaultExpanded
        />
      )}
      <Dialog.Footer>
        <Button variant="tertiary" onClick={handleClose}>
          Back
        </Button>
        <Button
          onClick={deploySpecUpdate}
          disabled={!file || isDeploying || !documentSlug}
        >
          {isDeploying && <Spinner />}
          {isDeploying ? "Deploying..." : "Deploy"}
        </Button>
      </Dialog.Footer>
    </>
  );
}
