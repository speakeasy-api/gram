import { Button } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { ToolSelector } from "./ToolSelect";

export const ToolSelectDialog = ({
  open,
  onOpenChange,
  toolsetSlug,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  toolsetSlug: string;
}) => {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="min-w-[80vw] h-[90vh] overflow-auto">
        <Dialog.Header>
          <Dialog.Title>Updating Toolset {toolsetSlug}</Dialog.Title>
          <Dialog.Description>
            Add or remove tools from the toolset.
          </Dialog.Description>
        </Dialog.Header>
        <ToolSelector toolsetSlug={toolsetSlug} />
        <Dialog.Footer>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Done
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
};
