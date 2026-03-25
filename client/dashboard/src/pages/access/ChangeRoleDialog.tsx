import { AnyField } from "@/components/moon/any-field";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Dialog } from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { Button } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { MOCK_ROLES } from "./mock-data";
import type { Member } from "./types";

interface ChangeRoleDialogProps {
  member: Member | null;
  onOpenChange: (open: boolean) => void;
}

export function ChangeRoleDialog({
  member,
  onOpenChange,
}: ChangeRoleDialogProps) {
  const [selectedRole, setSelectedRole] = useState<string | undefined>(
    undefined,
  );

  // Sync selected role when member changes
  const currentRole = selectedRole ?? member?.roleId;

  const handleUpdate = () => {
    // TODO: call API when backend is implemented
    onOpenChange(false);
  };

  return (
    <Dialog open={!!member} onOpenChange={onOpenChange}>
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header>
          <Dialog.Title>Change Role</Dialog.Title>
          <Dialog.Description>
            Update the role assignment for this team member.
          </Dialog.Description>
        </Dialog.Header>

        {member && (
          <div className="space-y-4 py-2">
            <div className="flex items-center gap-3 rounded-md border border-border p-3">
              <Avatar className="h-9 w-9">
                <AvatarFallback className="text-sm">
                  {member.name
                    .split(" ")
                    .map((n) => n[0])
                    .join("")
                    .toUpperCase()
                    .slice(0, 2)}
                </AvatarFallback>
              </Avatar>
              <div>
                <Type variant="body" className="font-medium">
                  {member.name}
                </Type>
                <Type variant="body" className="text-muted-foreground text-sm">
                  {member.email}
                </Type>
              </div>
            </div>

            <AnyField
              label="Role"
              render={() => (
                <Select value={currentRole} onValueChange={setSelectedRole}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select a role" />
                  </SelectTrigger>
                  <SelectContent>
                    {MOCK_ROLES.map((role) => (
                      <SelectItem key={role.id} value={role.id}>
                        {role.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />

            <div className="flex justify-end gap-2 pt-2">
              <Button variant="secondary" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button onClick={handleUpdate}>Update Role</Button>
            </div>
          </div>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
