import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { AccessibleMCPServer } from "@gram/client/models/components/accessiblemcpserver.js";
import {
  useResourceAudience,
  invalidateAllResourceAudience,
} from "@gram/client/react-query/resourceAudience.js";
import { invalidateAllExplainResourceAccess } from "@gram/client/react-query/explainResourceAccess.js";
import { invalidateAllIdentityAccess } from "@gram/client/react-query/identityAccess.js";
import { allMemberRoleIds } from "@/lib/member-roles";
import { invalidateAllMembers } from "@gram/client/react-query/members.js";
import { useUpdateMemberRolesMutation } from "@gram/client/react-query/updateMemberRoles.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { invalidateAllRoles } from "@gram/client/react-query/roles.js";
import { useSetResourceAudienceMutation } from "@gram/client/react-query/setResourceAudience.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { buildAccessRows, scopeState } from "../mcp/access/accessRows";
import { assignmentExclusions } from "./assignmentExclusions";

// Platform MCP inspection already exposes server/member access. Exclusion
// writes stay in this administrator dialog: the existing access-role mutation
// tools accept allow rules only and have no exclusion-confirmation contract.
// Do not route exclusions through those tools and silently widen their schema.
export function RemoveAssignmentDialog({
  server,
  member,
  onClose,
}: {
  server: AccessibleMCPServer;
  member: AccessMember;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const audience = useResourceAudience(
    { resourceKind: "mcp", resourceId: server.id },
    undefined,
    { throwOnError: false },
  );
  const roles = useRoles(undefined, undefined, { throwOnError: false });
  const [removedRoleId, setRemovedRoleId] = useState<string>();
  const memberRoles = new Set(allMemberRoleIds(member));
  const heldRoles =
    roles.data?.roles.filter((role) => memberRoles.has(role.id)) ?? [];
  const [selected, setSelected] = useState<string[]>([]);
  const [error, setError] = useState<string>();
  const roleRows = buildAccessRows(audience.data?.entries ?? []).filter(
    (row) =>
      row.kind === "role" &&
      scopeState(row, "use").granted &&
      heldRoles.some((role) => role.principalUrn === row.principalUrn),
  );
  const userUrn = member.principalUrn;
  const validTargets = new Set([
    ...roleRows.map((row) => row.principalUrn),
    userUrn,
  ]);
  const onSuccess = async () => {
    toast.success("Access changes saved");
    onClose();
    await Promise.all([
      invalidateAllResourceAudience(queryClient),
      invalidateAllExplainResourceAccess(queryClient),
      invalidateAllIdentityAccess(queryClient),
      invalidateAllRoles(queryClient),
      invalidateAllMembers(queryClient),
      queryClient.invalidateQueries({ queryKey: ["mcpAssignments"] }),
    ]);
  };
  const onError = (failure: unknown) =>
    setError(
      failure instanceof Error
        ? failure.message
        : "Couldn’t save access changes. Reload and try again.",
    );
  const mutation = useSetResourceAudienceMutation({ onSuccess, onError });
  const membership = useUpdateMemberRolesMutation({ onSuccess, onError });
  const pending = mutation.isPending || membership.isPending;
  const toggle = (principal: string, checked: boolean) => {
    setRemovedRoleId(undefined);
    setSelected((previous) =>
      checked
        ? [...previous, principal]
        : previous.filter((value) => value !== principal),
    );
  };
  const save = () => {
    if (pending) return;
    if (removedRoleId) {
      if (
        !member.roleIds.includes(removedRoleId) ||
        member.directoryRoleIds.includes(removedRoleId) ||
        member.roleIds.length <= 1
      )
        return;
      setError(undefined);
      membership.mutate({
        request: {
          updateMemberRolesForm: {
            userId: member.id,
            roleIds: member.roleIds.filter((id) => id !== removedRoleId),
          },
        },
      });
      return;
    }
    if (
      !audience.data ||
      audience.isFetching ||
      !selected.length ||
      selected.some((value) => !validTargets.has(value))
    )
      return;
    setError(undefined);
    mutation.mutate({
      request: {
        setResourceAudienceForm: {
          resourceKind: "mcp",
          resourceId: server.id,
          expectedVersion: audience.data.version,
          entries: assignmentExclusions(audience.data.entries, selected),
        },
      },
    });
  };
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <Dialog.Content className="sm:max-w-xl">
        <Dialog.Header>
          <Dialog.Title>
            Remove access to {server.name || "this server"}
          </Dialog.Title>
          <Dialog.Description>
            Manage connection access for {member.name || member.email}: block a
            role from this server, remove this person from a role, or block only
            this person.
          </Dialog.Description>
        </Dialog.Header>
        <div className="space-y-4">
          <Text small muted>
            Role exclusions apply to everyone with that role. People granted
            this server directly can keep access; blocking the user overrides
            their direct access too. Removing a person from a role removes all
            permissions that role grants across the organization.
          </Text>
          {audience.isLoading || roles.isLoading ? (
            <Text small muted>
              Loading roles with access…
            </Text>
          ) : audience.isError || roles.isError ? (
            <div role="alert">
              <Text small>Couldn’t load the current access rules.</Text>
              <Button
                variant="secondary"
                onClick={() =>
                  void Promise.all([audience.refetch(), roles.refetch()])
                }
              >
                <Button.Text>Retry</Button.Text>
              </Button>
            </div>
          ) : (
            <>
              <div className="divide-border max-h-[35vh] divide-y overflow-y-auto">
                {roleRows.length === 0 && (
                  <Text small muted>
                    No roles currently grant access. You can still block this
                    user.
                  </Text>
                )}
                {roleRows.map((row) => (
                  <div key={row.principalUrn} className="space-y-2 py-3">
                    <Text className="text-sm font-medium">
                      {row.displayName}
                    </Text>
                    <label className="flex cursor-pointer items-start gap-3">
                      <Checkbox
                        aria-label={`${row.displayName}: block connection access`}
                        checked={selected.includes(row.principalUrn)}
                        disabled={pending || audience.isFetching}
                        onCheckedChange={(checked) =>
                          toggle(row.principalUrn, checked === true)
                        }
                      />
                      <span>
                        <span className="text-muted-foreground block text-xs">
                          This role should not be able to connect to this
                          server.
                          {row.memberIds.length > 0 &&
                            ` Affects ${row.memberIds.length} ${row.memberIds.length === 1 ? "person" : "people"}.`}
                        </span>
                      </span>
                    </label>
                    {(() => {
                      const role = heldRoles.find(
                        (role) => role.principalUrn === row.principalUrn,
                      )!;
                      const mapped = member.directoryRoleIds.includes(role.id);
                      const lastRole = member.roleIds.length <= 1;
                      return (
                        <label className="flex items-start gap-3">
                          <Checkbox
                            aria-label={`Remove ${member.name || member.email} from ${row.displayName}`}
                            checked={removedRoleId === role.id}
                            disabled={
                              pending ||
                              mapped ||
                              lastRole ||
                              !member.roleIds.includes(role.id)
                            }
                            onCheckedChange={(checked) => {
                              setSelected([]);
                              setRemovedRoleId(
                                checked === true ? role.id : undefined,
                              );
                            }}
                          />
                          <span className="text-muted-foreground text-xs">
                            Remove {member.name || member.email} from this role.
                            {mapped
                              ? " Managed by directory mappings; change the mapping to remove membership."
                              : lastRole
                                ? " A person must keep at least one assigned role."
                                : " Removes all permissions granted by this role, across all servers."}
                          </span>
                        </label>
                      );
                    })()}
                  </div>
                ))}
              </div>
              <label className="border-border bg-muted flex cursor-pointer items-start gap-3 rounded-md border p-4">
                <Checkbox
                  checked={selected.includes(userUrn)}
                  disabled={pending || audience.isFetching}
                  onCheckedChange={(checked) =>
                    toggle(userUrn, checked === true)
                  }
                />
                <span>
                  <span className="block text-sm font-medium">
                    Block only this user
                  </span>
                  <span className="text-muted-foreground block text-xs">
                    Block {member.name || member.email} from connecting to this
                    server regardless of their roles or direct assignments.
                    Other people keep their access unless you also select a role
                    above.
                  </span>
                </span>
              </label>
            </>
          )}
          {error && (
            <div role="alert" className="space-y-2">
              <Text small className="text-destructive">
                {error}
              </Text>
              <Button
                variant="secondary"
                onClick={() => {
                  setRemovedRoleId(undefined);
                  setSelected([]);
                  setError(undefined);
                  void Promise.all([audience.refetch(), roles.refetch()]);
                }}
              >
                <Button.Text>Reload rules</Button.Text>
              </Button>
            </div>
          )}
        </div>
        <Dialog.Footer>
          <Button variant="secondary" disabled={pending} onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="destructive-primary"
            disabled={
              !audience.data ||
              audience.isError ||
              audience.isFetching ||
              pending ||
              roles.isLoading ||
              roles.isError ||
              (!selected.length && !removedRoleId) ||
              selected.some((value) => !validTargets.has(value))
            }
            onClick={save}
          >
            <Button.Text>
              {pending
                ? "Saving…"
                : removedRoleId
                  ? "Remove from role"
                  : "Remove access"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
