import { Input } from "@/components/ui/input";
import { slugify } from "@/lib/constants";
import { Dialog, Alert, Button } from "@speakeasy-api/moonshine";
import { Loader2Icon } from "lucide-react";
import { useState } from "react";
import { NamedAsset } from "./SourceCard";

interface RemoveSourceDialogContentProps {
  asset: NamedAsset;
  onConfirmRemoval: (
    assetIdOrSlug: string,
    type: "openapi" | "function" | "externalmcp",
  ) => Promise<void>;
  onClose: () => void;
}

export function RemoveSourceDialogContent({
  asset,
  onConfirmRemoval,
  onClose,
}: RemoveSourceDialogContentProps) {
  const [pending, setPending] = useState(false);
  const [inputMatches, setInputMatches] = useState(false);

  const sourceSlug = slugify(asset.name);
  const sourceLabel =
    asset.type === "openapi"
      ? "API Source"
      : asset.type === "function"
        ? "Function Source"
        : "External MCP";

  const handleConfirm = async () => {
    setPending(true);
    // For external MCPs, pass the slug; for others, pass the asset ID
    const identifier = asset.type === "externalmcp" ? asset.slug : asset.id;
    try {
      await onConfirmRemoval(identifier, asset.type);
    } finally {
      onClose();
    }
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

  const sourceTypeDescription =
    asset.type === "openapi"
      ? "API"
      : asset.type === "function"
        ? "gram function"
        : "external MCP";

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete {sourceLabel}</Dialog.Title>
        <Dialog.Description>
          This will permanently delete the {sourceTypeDescription} source and
          related resources such as tools within toolsets.
        </Dialog.Description>
      </Dialog.Header>
      <div className="grid gap-2">
        <span className="text-sm">
          To confirm, type "<strong>{sourceSlug}</strong>"
        </span>

        <Input onChange={(v) => setInputMatches(v === sourceSlug)} />
      </div>

      <Alert variant="warning" dismissible={false}>
        Deleting {sourceSlug} cannot be undone.
      </Alert>

      <Dialog.Footer>
        <DeleteButton />
      </Dialog.Footer>
    </>
  );
}
