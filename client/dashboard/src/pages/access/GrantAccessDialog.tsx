import { invalidateAllResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { RoleLink } from "@/components/role-link";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { Text } from "@/components/ui/Text";
import { Skeleton } from "@/components/ui/Skeleton";
import { Spinner } from "@/components/ui/Spinner";
import { useScopeCatalogue } from "@/hooks/useScopeCatalogue";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
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
import { useMemo, useState } from "react";
import { CreateRoleDialog } from "./CreateRoleDialog";
import { rolePermissionSummary } from "./reviewGrants";
import { RolePermissionPreview } from "./RolePermissionPreview";
import {
  breadthLabel,
  suggestRolesForScope,
  type RoleSuggestions,
  type SuggestedRole,
} from "./roleSuggestions";
import { scopeLabel, tasksNeeding } from "./scopeCatalogue";

interface GrantAccessDialogProps {
  /** User id of the member who requested access (from the email deep link). */
  userId: string;
  /** The scope that was requested (e.g. "mcp:connect"). */
  scope: string;
  /** Optional resource the scope was requested for; narrows role suggestions. */
  resourceId?: string;
  /** Project containing an MCP resource, when known. */
  projectId?: string;
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
 * Roles that give exactly the permission come first; roles that give more say
 * what else they add, and a role with only this permission can be created in
 * place when none fits.
 */
export function GrantAccessDialog({
  userId,
  scope,
  resourceId,
  projectId,
  onClose,
}: GrantAccessDialogProps): JSX.Element {
  const [assignedRoleId, setAssignedRoleId] = useState<string | null>(null);
  const [creatingRole, setCreatingRole] = useState(false);
  // A role that gives sensitive access is previewed before it is assigned.
  const [confirming, setConfirming] = useState<Role | null>(null);
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
  const {
    catalogue,
    tasks,
    isLoading: scopesLoading,
    isError: scopesError,
    refetch: refetchScopes,
  } = useScopeCatalogue();

  const member = useMemo(
    () => membersData?.members?.find((m) => m.id === userId) ?? null,
    [membersData?.members, userId],
  );

  const suggestions = useMemo(
    () =>
      suggestRolesForScope(
        rolesData?.roles ?? [],
        scope,
        catalogue,
        resourceId,
        projectId,
      ),
    [rolesData?.roles, scope, catalogue, resourceId, projectId],
  );

  const updateMemberRoles = useUpdateMemberRolesMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMembers(queryClient),
        invalidateAllRoles(queryClient),
        invalidateAllResourceAudience(queryClient),
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

  const requestAssign = (role: Role) => {
    if (rolePermissionSummary(role, catalogue).sensitive.length > 0) {
      setConfirming(role);
      return;
    }
    assignRole(role.id);
  };

  const isLoading = membersLoading || rolesLoading || scopesLoading;
  // Suggestions and sensitivity warnings are read off the catalogue, so a
  // failed catalogue fetch is a failed dialog, not an empty one.
  const loadFailed = membersError || rolesError || scopesError;
  const granted = assignedRoleId !== null && updateMemberRoles.isSuccess;
  const label = scopeLabel(catalogue, scope);
  const entry = catalogue.get(scope);
  const partOf = tasksNeeding(catalogue, tasks, scope);
  const labelFor = (slug: string) => scopeLabel(catalogue, slug);

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
            Assign a role that includes <strong>{label}</strong>{" "}
            <span className="font-mono text-xs">{scope}</span>.
            {entry?.description ? ` ${entry.description}` : ""}
          </Dialog.Description>
        </Dialog.Header>

        {partOf.length > 0 && (
          <ul className="space-y-1">
            {partOf.map((task) => {
              const alsoNeeds = task.scopes.filter((s) => s !== scope);
              return (
                <li key={task.slug}>
                  <Text muted small>
                    Part of <strong>{task.label}</strong>
                    {alsoNeeds.length > 0 && (
                      <>
                        , which also needs{" "}
                        {alsoNeeds
                          .map((s) => scopeLabel(catalogue, s))
                          .join(", ")}
                      </>
                    )}
                    .
                  </Text>
                </li>
              );
            })}
          </ul>
        )}

        {isLoading && (
          <div
            role="status"
            aria-label="Loading members, roles and permissions"
            className="space-y-2"
          >
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
            <Skeleton className="h-14 w-full" />
          </div>
        )}

        {loadFailed && (
          <div className="flex flex-col items-center gap-3 py-4">
            <Text variant="body" className="text-destructive text-sm">
              Failed to load members, roles and permissions.
            </Text>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                if (membersError) void refetchMembers();
                if (rolesError) void refetchRoles();
                if (scopesError) refetchScopes();
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

        {member && !isLoading && !loadFailed && (
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
                  {member.name} now has {label}.
                </Text>
              </div>
            ) : (
              <RoleSuggestionList
                member={member}
                scope={scope}
                label={label}
                suggestions={suggestions}
                labelFor={labelFor}
                pendingRoleId={
                  updateMemberRoles.isPending ? assignedRoleId : null
                }
                onAssign={requestAssign}
                onCreateRole={() => setCreatingRole(true)}
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

      {creatingRole && (
        <CreateRoleDialog
          open
          onOpenChange={(open) => {
            if (!open) setCreatingRole(false);
          }}
          defaultName={label}
          defaultScopes={[scope as Scope]}
          defaultResource={{ resourceId, projectId }}
          hideAssignments
          onRoleCreated={(role) => assignRole(role.id)}
        />
      )}

      {confirming && member && (
        <Dialog
          open
          onOpenChange={(open) => {
            if (!open) setConfirming(null);
          }}
        >
          <Dialog.Content className="sm:max-w-md">
            <Dialog.Header>
              <Dialog.Title>
                Assign &ldquo;{confirming.name}&rdquo; to {member.name}
              </Dialog.Title>
              <Dialog.Description>
                Check what the role gives before it is assigned.
              </Dialog.Description>
            </Dialog.Header>
            <RolePermissionPreview
              lead="This role gives"
              summary={rolePermissionSummary(confirming, catalogue)}
              labelFor={labelFor}
            />
            <Dialog.Footer>
              <Button variant="secondary" onClick={() => setConfirming(null)}>
                Cancel
              </Button>
              <Button
                onClick={() => {
                  assignRole(confirming.id);
                  setConfirming(null);
                }}
              >
                Assign role
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
      )}
    </Dialog>
  );
}

function RoleSuggestionList({
  member,
  scope,
  label,
  suggestions,
  labelFor,
  pendingRoleId,
  onAssign,
  onCreateRole,
}: {
  member: AccessMember;
  scope: string;
  label: string;
  suggestions: RoleSuggestions;
  labelFor: (slug: string) => string;
  pendingRoleId: string | null;
  onAssign: (role: Role) => void;
  onCreateRole: () => void;
}): JSX.Element {
  const { exact, broader } = suggestions;
  const nothingFits = exact.length === 0;

  const renderRole = (
    { role, breadth }: SuggestedRole,
    extra?: { count: number; sensitive: string[] },
  ) => {
    const alreadyAssigned = member.roleIds.includes(role.id);
    const reach = breadthLabel(scope, breadth);
    return (
      <li
        key={role.id}
        className="border-border flex items-center justify-between gap-3 border px-3 py-2"
      >
        <div className="min-w-0 space-y-0.5">
          <Text variant="body" className="text-sm font-medium">
            <RoleLink roleId={role.id}>{role.name}</RoleLink>
          </Text>
          {role.description && (
            <Text muted small className="truncate">
              {role.description}
            </Text>
          )}
          {reach && (
            <Text muted small>
              Grants {label} {reach}.
            </Text>
          )}
          {extra && (
            <div className="flex flex-wrap items-center gap-1.5">
              <Text muted small>
                Also grants {extra.count} more{" "}
                {extra.count === 1 ? "permission" : "permissions"}
                {extra.sensitive.length > 0 ? ", including" : ""}
              </Text>
              {extra.sensitive.map((slug) => (
                <Badge key={slug} variant="warning">
                  {labelFor(slug)}
                </Badge>
              ))}
            </div>
          )}
        </div>
        {alreadyAssigned ? (
          <Badge variant="neutral">Assigned</Badge>
        ) : (
          <Button
            variant={extra ? "secondary" : "primary"}
            size="sm"
            onClick={() => onAssign(role)}
            disabled={pendingRoleId !== null}
          >
            {pendingRoleId === role.id && (
              <Button.LeftIcon>
                <Spinner className="mr-0" />
              </Button.LeftIcon>
            )}
            <Button.Text>Assign</Button.Text>
          </Button>
        )}
      </li>
    );
  };

  return (
    <div className="space-y-3">
      {exact.length > 0 && (
        <div className="space-y-2">
          <Text muted small>
            Roles that give exactly this:
          </Text>
          <ul className="space-y-1.5">
            {exact.map((item) => renderRole(item))}
          </ul>
        </div>
      )}

      {nothingFits && (
        <Text muted small>
          No existing role gives only {label}.
        </Text>
      )}

      <Button
        variant={nothingFits ? "primary" : "secondary"}
        size="sm"
        onClick={onCreateRole}
        disabled={pendingRoleId !== null}
      >
        Create a role with only this permission
      </Button>

      {broader.length > 0 && (
        <div className="space-y-2">
          <Text muted small>
            Roles that give more than this:
          </Text>
          <ul className="space-y-1.5">
            {broader.map((item) =>
              renderRole(item, {
                count: item.extraScopes.length,
                sensitive: item.extraSensitive,
              }),
            )}
          </ul>
        </div>
      )}
    </div>
  );
}
