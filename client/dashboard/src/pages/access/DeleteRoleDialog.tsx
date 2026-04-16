import { Dialog } from "@/components/ui/dialog";
import { Button } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { PropsWithChildren } from "react";

export interface DeleteRoleDialogProps extends PropsWithChildren {
  isOpen: boolean;
  onOpenChange?: (open: boolean) => void;
  handleDeleteRole: () => void;
  handleCancel: () => void;
  role: { name: string } | null;
}

export const DeleteRoleDialog = ({
  isOpen,
  onOpenChange,
  handleDeleteRole,
  handleCancel,
  role,
  children,
}: DeleteRoleDialogProps) => {
  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <Dialog.Trigger asChild>{children}</Dialog.Trigger>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete Role</Dialog.Title>
        </Dialog.Header>
        <div className="space-y-4 py-4">
          <Type variant="body">
            <code className="bg-muted rounded px-1 py-0.5 font-mono font-bold">
              {role?.name}
            </code>{" "}
            will be permanently deleted. This action cannot be undone.
          </Type>
          <div className="flex justify-end space-x-2">
            <Button variant="secondary" onClick={handleCancel}>
              Cancel
            </Button>
            <Button variant="destructive-primary" onClick={handleDeleteRole}>
              Delete Role
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
};
