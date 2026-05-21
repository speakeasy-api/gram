import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import { invalidateAllMembers } from "@gram/client/react-query/members.js";
import {
  invalidateAllRoles,
  useRoles,
} from "@gram/client/react-query/roles.js";
import { useUpdateMemberRoleMutation } from "@gram/client/react-query/updateMemberRole.js";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { cn } from "@/lib/utils";

interface ChangeRoleDialogProps {
  member: AccessMember | null;
  onOpenChange: (open: boolean) => void;
}

export function ChangeRoleDialog({
  member,
  onOpenChange,
}: ChangeRoleDialogProps) {
  const [selectedRoleIds, setSelectedRoleIds] = useState<string[]>([]);
  const queryClient = useQueryClient();
  const { data: rolesData } = useRoles();
  const roles = rolesData?.roles ?? [];

  const updateMemberRole = useUpdateMemberRoleMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMembers(queryClient),
        invalidateAllRoles(queryClient),
      ]);
      onOpenChange(false);
    },
  });

  // Reset selected roles when the dialog opens for a different member
  useEffect(() => {
    setSelectedRoleIds(member?.roleIds ?? []);
  }, [member]);

  const toggleRole = (roleId: string) => {
    setSelectedRoleIds((prev) => {
      if (prev.includes(roleId)) {
        // Don't allow deselecting the last role
        if (prev.length <= 1) return prev;
        return prev.filter((id) => id !== roleId);
      }
      return [...prev, roleId];
    });
  };

  const hasChanged =
    member &&
    (selectedRoleIds.length !== member.roleIds.length ||
      selectedRoleIds.some((id) => !member.roleIds.includes(id)));

  const handleUpdate = () => {
    if (!member || selectedRoleIds.length === 0) return;
    updateMemberRole.mutate({
      request: {
        updateMemberRoleForm: {
          userId: member.id,
          roleIds: selectedRoleIds,
        },
      },
    });
  };

  return (
    <Dialog open={!!member} onOpenChange={onOpenChange}>
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header>
          <Dialog.Title>Manage Roles</Dialog.Title>
          <Dialog.Description>
            Select one or more roles for this team member.
          </Dialog.Description>
        </Dialog.Header>

        {member && (
          <div className="space-y-4 py-2">
            <div className="border-border flex items-center gap-3 rounded-md border p-3">
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

            <div className="space-y-1">
              {roles.map((role) => {
                const isSelected = selectedRoleIds.includes(role.id);
                return (
                  <button
                    key={role.id}
                    type="button"
                    onClick={() => toggleRole(role.id)}
                    className={cn(
                      "flex w-full cursor-pointer items-center gap-3 rounded-md border px-3 py-2.5 text-left transition-colors",
                      isSelected
                        ? "border-primary/30 bg-primary/5"
                        : "border-border hover:bg-accent",
                    )}
                  >
                    <div
                      className={cn(
                        "flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border",
                        isSelected
                          ? "border-primary bg-primary text-primary-foreground"
                          : "border-muted-foreground/30",
                      )}
                    >
                      {isSelected && <Check className="h-3 w-3" />}
                    </div>
                    <div className="min-w-0 flex-1">
                      <Type variant="body" className="font-medium">
                        {role.name}
                      </Type>
                      {role.description && (
                        <Type
                          variant="body"
                          className="text-muted-foreground truncate text-xs"
                        >
                          {role.description}
                        </Type>
                      )}
                    </div>
                  </button>
                );
              })}
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <Button variant="secondary" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button
                onClick={handleUpdate}
                disabled={
                  updateMemberRole.isPending ||
                  selectedRoleIds.length === 0 ||
                  !hasChanged
                }
              >
                {updateMemberRole.isPending && (
                  <Button.LeftIcon>
                    <Loader2 className="h-4 w-4 animate-spin" />
                  </Button.LeftIcon>
                )}
                <Button.Text>
                  {updateMemberRole.isPending ? "Updating…" : "Update Roles"}
                </Button.Text>
              </Button>
            </div>
          </div>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
