import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import { invalidateAllMembers } from "@gram/client/react-query/members.js";
import {
  invalidateAllRoles,
  useRoles,
} from "@gram/client/react-query/roles.js";
import { useUpdateMemberRolesMutation } from "@gram/client/react-query/updateMemberRoles.js";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  addRoleToSelection,
  removeRoleFromSelection,
  getUnselectedRoles,
  isUpdateDisabled,
} from "./changeRoleState";

interface ChangeRoleDialogProps {
  member: AccessMember | null;
  onOpenChange: (open: boolean) => void;
}

export function ChangeRoleDialog({
  member,
  onOpenChange,
}: ChangeRoleDialogProps): JSX.Element {
  const [selectedRoleIds, setSelectedRoleIds] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const queryClient = useQueryClient();
  const { data: rolesData } = useRoles();
  const roles = useMemo(() => rolesData?.roles ?? [], [rolesData?.roles]);
  const roleById = useMemo(() => new Map(roles.map((r) => [r.id, r])), [roles]);

  const updateMemberRoles = useUpdateMemberRolesMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMembers(queryClient),
        invalidateAllRoles(queryClient),
      ]);
      onOpenChange(false);
    },
  });

  useEffect(() => {
    setSelectedRoleIds(member?.roleIds ?? []);
    setSearch("");
  }, [member]);

  const addRole = (roleId: string) => {
    setSelectedRoleIds((prev) => addRoleToSelection(prev, roleId));
    setSearch("");
  };

  const removeRole = (roleId: string) => {
    setSelectedRoleIds((prev) => removeRoleFromSelection(prev, roleId));
  };

  const unselectedRoles = getUnselectedRoles(roles, selectedRoleIds);

  const updateDisabled = isUpdateDisabled({
    isPending: updateMemberRoles.isPending,
    selectedIds: selectedRoleIds,
    originalIds: member?.roleIds ?? [],
  });

  const handleUpdate = () => {
    if (!member || selectedRoleIds.length === 0) return;
    updateMemberRoles.mutate({
      request: {
        updateMemberRolesForm: {
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
            Search and select roles for this team member.
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

            {/* Role typeahead */}
            <Command
              className="border-border h-auto rounded-md border [&_[data-slot=command-input-wrapper]]:h-9 [&_[data-slot=command-input]]:h-9"
              shouldFilter
            >
              <CommandInput
                placeholder="Search roles…"
                value={search}
                onValueChange={setSearch}
              />
              {unselectedRoles.length > 0 ? (
                <CommandList className="max-h-40 min-h-40 p-1 outline-none">
                  <CommandEmpty className="py-3 text-center text-sm">
                    No matching roles.
                  </CommandEmpty>
                  <CommandGroup>
                    {unselectedRoles.map((role) => (
                      <CommandItem
                        key={role.id}
                        value={role.name}
                        onSelect={() => addRole(role.id)}
                      >
                        <div className="min-w-0 flex-1">
                          <span className="text-sm font-medium">
                            {role.name}
                          </span>
                          {role.description && (
                            <span className="text-muted-foreground ml-2 text-xs">
                              {role.description}
                            </span>
                          )}
                        </div>
                      </CommandItem>
                    ))}
                  </CommandGroup>
                </CommandList>
              ) : (
                <div className="text-muted-foreground flex min-h-40 items-center justify-center px-3 text-xs">
                  All roles assigned.
                </div>
              )}
            </Command>

            {/* Selected role chips */}
            <div className="border-border flex min-h-16 flex-wrap content-start gap-1.5 rounded-md border border-dashed p-2">
              {selectedRoleIds.length > 0 ? (
                selectedRoleIds.map((id) => {
                  const role = roleById.get(id);
                  if (!role) return null;
                  return (
                    <Badge key={id} variant="secondary" className="gap-1 pr-1">
                      {role.name}
                      <button
                        type="button"
                        onClick={() => removeRole(id)}
                        disabled={selectedRoleIds.length <= 1}
                        className="hover:bg-muted-foreground/20 ml-0.5 rounded-sm p-0.5 transition-colors disabled:cursor-not-allowed disabled:opacity-30"
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </Badge>
                  );
                })
              ) : (
                <span className="text-muted-foreground/50 m-auto text-xs">
                  No roles assigned
                </span>
              )}
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <Button variant="secondary" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button onClick={handleUpdate} disabled={updateDisabled}>
                {updateMemberRoles.isPending && (
                  <Button.LeftIcon>
                    <Loader2 className="h-4 w-4 animate-spin" />
                  </Button.LeftIcon>
                )}
                <Button.Text>
                  {updateMemberRoles.isPending ? "Updating…" : "Update Roles"}
                </Button.Text>
              </Button>
            </div>
          </div>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
