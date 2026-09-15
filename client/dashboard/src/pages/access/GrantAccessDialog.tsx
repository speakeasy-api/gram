import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { Text } from "@/components/ui/Text";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import {
  invalidateAllMembers,
  useMembers,
} from "@gram/client/react-query/members.js";
import {
  invalidateAllRoles,
  useRoles,
} from "@gram/client/react-query/roles.js";
import { useUpdateMemberRolesMutation } from "@gram/client/react-query/updateMemberRoles.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useMemo, useState } from "react";
import { rolesCoveringScope } from "./roleSuggestions";

interface GrantAccessDialogProps {
  /** User id of the member who requested access (from the email deep link). */
  userId: string;
  /** The scope that was requested (e.g. "mcp:connect"). */
  scope: string;
  /** Optional resource the scope was requested for; narrows role suggestions. */
  resourceId?: string;
  onClose: () => void;
}

function memberInitials(member: AccessMember): string {
  return member.name
    .split(" ")
    .map((n) => n[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
}

/**
 * One-click grant dialog opened from an access request email deep link
 * (?grant_user=<id>&scope=<scope>). Shows the requester and the roles whose
 * grants satisfy the requested scope; assigning a role is a single click.
 */
export function GrantAccessDialog({
  userId,
  scope,
  resourceId,
  onClose,
}: GrantAccessDialogProps): JSX.Element {
  const [assignedRoleId, setAssignedRoleId] = useState<string | null>(null);
  const queryClient = useQueryClient();
  const {
    data: membersData,
    isLoading: membersLoading,
    isError: membersError,
    refetch: refetchMembers,
  } = useMembers();
  const {
    data: rolesData,
    isLoading: rolesLoading,
    isError: rolesError,
    refetch: refetchRoles,
  } = useRoles();

  const member = useMemo(
    () => membersData?.members?.find((m) => m.id === userId) ?? null,
    [membersData?.members, userId],
  );

  const suggestedRoles = useMemo(
    () => rolesCoveringScope(rolesData?.roles ?? [], scope, resourceId),
    [rolesData?.roles, scope, resourceId],
  );

  const updateMemberRoles = useUpdateMemberRolesMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMembers(queryClient),
        invalidateAllRoles(queryClient),
      ]);
    },
  });

  const assignRole = (roleId: string) => {
    if (!member) return;
    setAssignedRoleId(roleId);
    updateMemberRoles.mutate(
      {
        request: {
          updateMemberRolesForm: {
            userId: member.id,
            roleIds: [...new Set([...member.roleIds, roleId])],
          },
        },
      },
      { onError: () => setAssignedRoleId(null) },
    );
  };

  const isLoading = membersLoading || rolesLoading;
  const loadFailed = membersError || rolesError;
  const granted = assignedRoleId !== null && updateMemberRoles.isSuccess;

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header>
          <Dialog.Title>Grant Access</Dialog.Title>
          {/* Opened from access-request emails and from server team-access
              rows alike, so the copy names the permission without assuming a
              request. */}
          <Dialog.Description>
            Assign a role that includes the{" "}
            <span className="font-mono text-xs">{scope}</span> permission.
          </Dialog.Description>
        </Dialog.Header>

        {isLoading && (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </div>
        )}

        {loadFailed && (
          <div className="flex flex-col items-center gap-3 py-4">
            <Text variant="body" className="text-destructive text-sm">
              Failed to load members and roles.
            </Text>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                if (membersError) void refetchMembers();
                if (rolesError) void refetchRoles();
              }}
            >
              Retry
            </Button>
          </div>
        )}

        {!isLoading && !loadFailed && !member && (
          <Text muted small className="py-4">
            This member is no longer part of the organization.
          </Text>
        )}

        {member && !loadFailed && (
          <div className="space-y-4 py-2">
            <div className="border-border flex items-center gap-3 border p-3">
              <Avatar className="h-9 w-9">
                {member.photoUrl && (
                  <AvatarImage src={member.photoUrl} alt={member.name} />
                )}
                <AvatarFallback className="text-sm">
                  {memberInitials(member)}
                </AvatarFallback>
              </Avatar>
              <div>
                <Text variant="body" className="font-medium">
                  {member.name}
                </Text>
                <Text variant="body" className="text-muted-foreground text-sm">
                  {member.email}
                </Text>
              </div>
            </div>

            {granted ? (
              <div className="flex flex-col items-center gap-1 py-2">
                <div className="text-default-success flex items-center gap-1.5 text-sm font-medium">
                  <Icon name="check" className="size-4" />
                  Access granted
                </div>
                <Text muted small>
                  {member.name} now has the requested permission.
                </Text>
              </div>
            ) : (
              <RoleSuggestionList
                member={member}
                scope={scope}
                suggestedRoles={suggestedRoles}
                pendingRoleId={
                  updateMemberRoles.isPending ? assignedRoleId : null
                }
                onAssign={assignRole}
              />
            )}

            {updateMemberRoles.isError && (
              <Text variant="body" className="text-destructive text-sm">
                Failed to assign the role. Please try again.
              </Text>
            )}

            <div className="flex justify-end pt-2">
              <Button variant="secondary" onClick={onClose}>
                {granted ? "Done" : "Cancel"}
              </Button>
            </div>
          </div>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function RoleSuggestionList({
  member,
  scope,
  suggestedRoles,
  pendingRoleId,
  onAssign,
}: {
  member: AccessMember;
  scope: string;
  suggestedRoles: ReturnType<typeof rolesCoveringScope>;
  pendingRoleId: string | null;
  onAssign: (roleId: string) => void;
}): JSX.Element {
  if (suggestedRoles.length === 0) {
    return (
      <Text muted small>
        No existing role includes <span className="font-mono">{scope}</span>.
        Create a role with that permission on the Roles tab, then assign it.
      </Text>
    );
  }

  return (
    <div className="space-y-2">
      <Text muted small>
        Roles that include this permission:
      </Text>
      <ul className="space-y-1.5">
        {suggestedRoles.map((role) => {
          const alreadyAssigned = member.roleIds.includes(role.id);
          return (
            <li
              key={role.id}
              className="border-border flex items-center justify-between gap-3 border px-3 py-2"
            >
              <div className="min-w-0">
                <Text variant="body" className="text-sm font-medium">
                  {role.name}
                </Text>
                {role.description && (
                  <Text muted small className="truncate">
                    {role.description}
                  </Text>
                )}
              </div>
              {alreadyAssigned ? (
                <Badge variant="neutral">Assigned</Badge>
              ) : (
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => onAssign(role.id)}
                  disabled={pendingRoleId !== null}
                >
                  {pendingRoleId === role.id && (
                    <Button.LeftIcon>
                      <Loader2 className="h-4 w-4 animate-spin" />
                    </Button.LeftIcon>
                  )}
                  <Button.Text>Assign</Button.Text>
                </Button>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
