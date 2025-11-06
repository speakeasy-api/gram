import { Button, Combobox, Dialog, Icon } from "@speakeasy-api/moonshine";
// import { Dialog } from "@/components/ui/dialog";
import { NamedAsset } from "./SourceCard";
import {
  useCreateEnvironmentMutation,
  useListEnvironments,
} from "@gram/client/react-query";
import { useState } from "react";

interface ApplyEnvironmentDialogContentProps {
  asset: NamedAsset;
}

export function ApplyEnvironmentDialogContent({
  asset,
}: ApplyEnvironmentDialogContentProps) {
  const result = useListEnvironments();
  const mutation = useCreateEnvironmentMutation();

  const [envQuery, setEnvQuery] = useState<string | undefined>(undefined);
  const handleConfirm = () => {};

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Apply Environment</Dialog.Title>
        <Dialog.Description>
          Apply environment configuration to {asset.name}
        </Dialog.Description>
      </Dialog.Header>

      <div className="py-4">
        <Combobox
          value={envQuery ?? ""}
          placeholder="select or create"
          options={(result.data?.environments ?? []).map((env) => ({
            value: env.id,
            label: env.name,
          }))}
          onValueChange={setEnvQuery}
          createOptions={{
            renderCreatePrompt: (query) => (
              <div
                className="flex items-center gap-2"
                onClick={() => alert("")}
              >
                <Icon name="plus" /> Create "{query}"
              </div>
            ),
            handleCreate: (query) => {
              alert(`Creating environment "${query}"`);
            },
          }}
        />
      </div>

      <Dialog.Footer>
        <Button onClick={handleConfirm} variant="primary">
          Apply Environment
        </Button>
      </Dialog.Footer>
    </>
  );
}
