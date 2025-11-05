import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { slugify } from "@/lib/constants";
import { Alert, Button } from "@speakeasy-api/moonshine";
import { Loader2Icon } from "lucide-react";
import { useState } from "react";
import { NamedAsset } from "./SourceCard";

interface RemoveSourceDialogContentProps {
  asset: NamedAsset;
  onConfirmRemoval: (
    assetId: string,
    type: "openapi" | "function",
  ) => Promise<void>;
}

export function RemoveSourceDialogContent({
  asset,
  onConfirmRemoval,
}: RemoveSourceDialogContentProps) {
  const [pending, setPending] = useState(false);
  const [inputMatches, setInputMatches] = useState(false);

  const sourceSlug = slugify(asset.name);
  const sourceLabel =
    asset.type === "openapi" ? "API Source" : "Function Source";

  const handleConfirm = async () => {
    setPending(true);
    await onConfirmRemoval(asset.id, asset.type);
    setPending(false);
    setInputMatches(false);
  };

  const DeleteButton = () => {
    if (pending) {
      return (
        <Button disabled variant="destructive-primary">
          <Button.LeftIcon>
            <Loader2Icon className="size-4 animate-spin" />
          </Button.LeftIcon>
          <Button.Text>Deleting {sourceLabel}</Button.Text>
        </Button>
      );
    }

    return (
      <Button
        disabled={!inputMatches}
        variant="destructive-primary"
        onClick={handleConfirm}
      >
        Delete {sourceLabel}
      </Button>
    );
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete {sourceLabel}</Dialog.Title>
        <Dialog.Description>
          This will permanently delete the{" "}
          {asset.type === "openapi" ? "API" : "gram function"} source and
          related resources such as tools within toolsets.
        </Dialog.Description>
      </Dialog.Header>
      <div className="grid gap-2">
        <span className="text-sm">
          To confirm, type "<strong>{sourceSlug}</strong>"
        </span>

        <Input onChange={(v) => setInputMatches(v === sourceSlug)} />
      </div>

      <Alert variant="error" dismissible={false}>
        Deleting {sourceSlug} cannot be undone.
      </Alert>

      <Dialog.Footer>
        <DeleteButton />
      </Dialog.Footer>
    </>
  );
}
