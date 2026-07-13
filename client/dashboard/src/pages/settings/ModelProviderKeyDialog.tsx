import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { invalidateAllModelProviderKeys } from "@gram/client/react-query/modelProviderKeys.js";
import { useUpsertModelProviderKeyMutation } from "@gram/client/react-query/upsertModelProviderKey.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { MODEL_KEY_PROVIDER, type ModelKeySlot } from "./model-key-slots";

export function ModelProviderKeyDialog({
  slot,
  hasExistingKey,
  onClose,
}: {
  slot: ModelKeySlot;
  hasExistingKey: boolean;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [apiKey, setApiKey] = useState("");
  const fieldId = `model-key-${slot.slot}`;

  const { mutate: upsert, isPending: isMutating } =
    useUpsertModelProviderKeyMutation({
      onSuccess: () => {
        toast.success(`${slot.name} key saved`);
        void invalidateAllModelProviderKeys(queryClient);
        onClose();
      },
      onError: (err) => {
        toast.error(`Failed to save key: ${err.message}`);
      },
    });

  const canSave = apiKey.trim() !== "" && !isMutating;

  const save = () => {
    if (!canSave) return;
    upsert({
      request: {
        upsertKeyRequestBody: {
          slot: slot.slot,
          provider: MODEL_KEY_PROVIDER,
          apiKey: apiKey.trim(),
          enabled: true,
        },
      },
    });
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>
            {hasExistingKey
              ? `Replace ${slot.name} key`
              : `Set ${slot.name} key`}
          </Dialog.Title>
          <Dialog.Description>{slot.description}</Dialog.Description>
        </Dialog.Header>
        <div className="flex flex-col gap-2">
          <Label htmlFor={fieldId}>OpenRouter API key</Label>
          <Input
            id={fieldId}
            type="password"
            placeholder={
              hasExistingKey ? "•••••• (saved)" : "OpenRouter API key"
            }
            value={apiKey}
            onChange={setApiKey}
            onEnter={save}
            disabled={isMutating}
            autoFocus
          />
          <Type muted small>
            The key is validated with the provider and stored encrypted. It is
            never displayed again.
          </Type>
        </div>
        <Dialog.Footer>
          <Button variant="secondary" onClick={onClose} disabled={isMutating}>
            Cancel
          </Button>
          <Button onClick={save} disabled={!canSave}>
            {isMutating ? "Validating..." : "Save"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
