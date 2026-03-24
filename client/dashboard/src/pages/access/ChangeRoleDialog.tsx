import { AnyField } from "@/components/moon/any-field";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Dialog } from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import { invalidateAllListMembers } from "@gram/client/react-query/listMembers.js";
import {
  invalidateAllListRoles,
  useListRoles,
} from "@gram/client/react-query/listRoles.js";
import { useUpdateMemberRoleMutation } from "@gram/client/react-query/updateMemberRole.js";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useState } from "react";

interface ChangeRoleDialogProps {
  member: AccessMember | null;
  onOpenChange: (open: boolean) => void;
}

export function ChangeRoleDialog({
  member,
  onOpenChange,
}: ChangeRoleDialogProps) {
  const [selectedRole, setSelectedRole] = useState<string | undefined>(
    undefined,
  );
  const queryClient = useQueryClient();
  const { data: rolesData } = useListRoles();
  const roles = rolesData?.roles ?? [];

  const updateMemberRole = useUpdateMemberRoleMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllListMembers(queryClient),
        invalidateAllListRoles(queryClient),
      ]);
      onOpenChange(false);
    },
  });

  // Sync selected role when member changes
  const currentRole = selectedRole ?? member?.roleId;

  const handleUpdate = () => {
    if (!member || !currentRole) return;
    updateMemberRole.mutate({
      request: {
        updateMemberRoleForm: {
          userId: member.id,
          roleId: currentRole,
        },
      },
    });
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
                {member.photoUrl && (
                  <AvatarImage src={member.photoUrl} alt={member.name} />
                )}
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
              optionality="hidden"
              render={() => (
                <Select value={currentRole} onValueChange={setSelectedRole}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select a role" />
                  </SelectTrigger>
                  <SelectContent>
                    {roles.map((role) => (
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
              <Button
                onClick={handleUpdate}
                disabled={
                  updateMemberRole.isPending ||
                  !currentRole ||
                  currentRole === member?.roleId
                }
              >
                {updateMemberRole.isPending && (
                  <Button.LeftIcon>
                    <Loader2 className="h-4 w-4 animate-spin" />
                  </Button.LeftIcon>
                )}
                <Button.Text>
                  {updateMemberRole.isPending ? "Updating…" : "Update Role"}
                </Button.Text>
              </Button>
            </div>
          </div>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
