import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { Environment } from "@gram/client/models/components/environment.js";
import { Button } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { useCloneEnvironment } from "./useEnvironmentActions";

type Props = {
  source: Environment | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function CloneEnvironmentDialog({ source, open, onOpenChange }: Props) {
  const [name, setName] = useState(source ? `${source.name} (copy)` : "");
  const [copyValues, setCopyValues] = useState(false);

  const { clone, isPending } = useCloneEnvironment({
    onSuccess: () => onOpenChange(false),
  });

  const submit = () => {
    if (!source || !name.trim() || isPending) return;
    clone({
      sourceSlug: source.slug,
      newName: name.trim(),
      copyValues,
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Clone environment</Dialog.Title>
          <Dialog.Description>
            {source
              ? `Create a copy of "${source.name}".`
              : "Create a copy of this environment."}
          </Dialog.Description>
        </Dialog.Header>

        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <Type variant="small" className="font-medium">
              New environment name
            </Type>
            <Input
              autoFocus
              value={name}
              onChange={setName}
              onEnter={submit}
              placeholder="Environment name"
              validate={(v) => v.trim().length > 0}
            />
          </div>

          <div className="flex items-start justify-between gap-4 rounded-lg border p-3">
            <div className="flex flex-col gap-1">
              <Type variant="small" className="font-medium">
                Copy stored secret values
              </Type>
              <Type small muted>
                Off: copies only variable names with empty placeholders. On:
                duplicates the encrypted secret values from the source.
              </Type>
            </div>
            <Switch
              checked={copyValues}
              onCheckedChange={setCopyValues}
              aria-label="Copy stored secret values"
            />
          </div>
        </div>

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={() => onOpenChange(false)}
            disabled={isPending}
          >
            Cancel
          </Button>
          <Button onClick={submit} disabled={!name.trim() || isPending}>
            {isPending ? "Cloning..." : "Clone"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
