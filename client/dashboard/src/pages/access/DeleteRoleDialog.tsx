import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import { Badge, Button } from "@/components/ui/moonshine";
import { ArrowRight } from "lucide-react";
import { PropsWithChildren } from "react";

export interface DeleteRoleDialogProps extends PropsWithChildren {
  isOpen: boolean;
  onOpenChange?: (open: boolean) => void;
  handleDeleteRole: () => void;
  handleCancel: () => void;
  role: Role | null;
  members: AccessMember[];
  defaultRole: Role | null;
}

export const DeleteRoleDialog = ({
  isOpen,
  onOpenChange,
  handleDeleteRole,
  handleCancel,
  role,
  members,
  defaultRole,
  children,
}: DeleteRoleDialogProps): JSX.Element => {
  const hasMembers = members.length > 0;
  const soleRoleMembers = role
    ? members.filter((m) => m.roleIds.length === 1)
    : [];

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <Dialog.Trigger asChild>{children}</Dialog.Trigger>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete Role</Dialog.Title>
        </Dialog.Header>
        <div className="space-y-4 py-4">
          {hasMembers ? (
            <Type variant="body">
              Are you sure?{" "}
              {soleRoleMembers.length > 0 && defaultRole ? (
                <>
                  {soleRoleMembers.length === members.length
                    ? "All affected members"
                    : `${soleRoleMembers.length} of ${members.length} affected member${members.length === 1 ? "" : "s"}`}{" "}
                  will fall back to the default role{" "}
                  <Badge
                    variant="neutral"
                    background={false}
                    size="sm"
                    className="font-mono text-[10px] uppercase"
                  >
                    {defaultRole.name}
                  </Badge>
                  . Members with other roles will simply lose this one.
                </>
              ) : (
                <>
                  This role will be removed from {members.length} member
                  {members.length === 1 ? "" : "s"}.
                </>
              )}
            </Type>
          ) : (
            <Type variant="body">
              <code className="bg-muted rounded px-1 py-0.5 font-mono font-bold">
                {role?.name}
              </code>{" "}
              will be permanently deleted. This action cannot be undone.
            </Type>
          )}

          {hasMembers && (
            <div className="border-border divide-border max-h-72 divide-y overflow-y-auto border">
              {members.map((member) => {
                const isOnlyRole = member.roleIds.length === 1;
                return (
                  <div
                    key={member.id}
                    className="flex items-center gap-3 px-3 py-2.5"
                  >
                    <Avatar className="h-7 w-7">
                      {member.photoUrl && (
                        <AvatarImage src={member.photoUrl} alt={member.name} />
                      )}
                      <AvatarFallback className="text-xs">
                        {member.name
                          .split(" ")
                          .map((n) => n[0])
                          .join("")
                          .toUpperCase()
                          .slice(0, 2)}
                      </AvatarFallback>
                    </Avatar>
                    <div className="min-w-0 flex-1 space-y-0.5">
                      <div className="flex items-center gap-2">
                        <Type variant="body" className="text-sm font-medium">
                          {member.name}
                        </Type>
                        {role && isOnlyRole && defaultRole && (
                          <div className="flex items-center gap-1">
                            <Badge
                              variant="neutral"
                              background={false}
                              size="sm"
                              className="font-mono text-[10px] uppercase line-through opacity-60"
                            >
                              {role.name}
                            </Badge>
                            <ArrowRight className="text-muted-foreground h-3 w-3 shrink-0" />
                            <Badge
                              variant="neutral"
                              background={false}
                              size="sm"
                              className="border-primary text-primary font-mono text-[10px] uppercase"
                            >
                              {defaultRole.name}
                            </Badge>
                          </div>
                        )}
                      </div>
                      <Type
                        variant="body"
                        className="text-muted-foreground text-xs"
                      >
                        {member.email}
                      </Type>
                    </div>
                  </div>
                );
              })}
            </div>
          )}

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
