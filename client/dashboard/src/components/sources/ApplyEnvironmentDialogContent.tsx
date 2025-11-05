import { Dialog } from "@/components/ui/dialog";
import { Button } from "@speakeasy-api/moonshine";
import { NamedAsset } from "./SourceCard";

interface ApplyEnvironmentDialogContentProps {
  asset: NamedAsset;
  onConfirm: (assetId: string) => void;
}

export function ApplyEnvironmentDialogContent({
  asset,
  onConfirm,
}: ApplyEnvironmentDialogContentProps) {
  const handleConfirm = () => {
    onConfirm(asset.id);
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Apply Environment</Dialog.Title>
        <Dialog.Description>
          Apply environment configuration to {asset.name}
        </Dialog.Description>
      </Dialog.Header>

      <div className="py-4">
        <p className="text-sm text-muted-foreground">
          This will apply the environment configuration to the selected source.
        </p>
      </div>

      <Dialog.Footer>
        <Button onClick={handleConfirm} variant="primary">
          Apply Environment
        </Button>
      </Dialog.Footer>
    </>
  );
}
