import { useMemo, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Column, Table } from "@speakeasy-api/moonshine";

import {
  invalidateAllPolicyAccessRequests,
  invalidateAllPolicyBypasses,
  useDecidePolicyAccessRequestMutation,
  usePolicyAccessRequests,
  usePolicyBypasses,
  useRevokePolicyBypassMutation,
  useRoles,
} from "@gram/client/react-query";
import type {
  PolicyAccessRequest,
  PolicyAccessTarget,
  PolicyBypassGrant,
  Role,
} from "@gram/client/models/components";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";

type GrantType = "requester" | "requester_roles" | "roles";

export function PolicyAccessRequestsContent() {
  const queryClient = useQueryClient();
  const { data: requestsData, isLoading: isLoadingRequests } =
    usePolicyAccessRequests({ status: "requested" });
  const { data: bypassesData, isLoading: isLoadingBypasses } =
    usePolicyBypasses();
  const { data: rolesData } = useRoles();
  const roles = useMemo(() => rolesData?.roles ?? [], [rolesData?.roles]);

  const invalidatePolicyAccess = () => {
    void invalidateAllPolicyAccessRequests(queryClient);
    void invalidateAllPolicyBypasses(queryClient);
  };

  const decide = useDecidePolicyAccessRequestMutation({
    onSuccess: invalidatePolicyAccess,
  });
  const revoke = useRevokePolicyBypassMutation({
    onSuccess: invalidatePolicyAccess,
  });

  const requests = requestsData?.requests ?? [];
  const bypasses = bypassesData?.bypasses ?? [];

  const requestColumns = useMemo<Column<PolicyAccessRequest>[]>(
    () => [
      {
        key: "requester",
        header: "Requester",
        width: "1.2fr",
        render: (request) => (
          <Identity
            primary={
              request.requesterEmail || request.requesterUserId || "Unknown"
            }
            secondary={request.requesterUserId}
          />
        ),
      },
      {
        key: "policy",
        header: "Policy",
        width: "1fr",
        render: (request) => (
          <Type className="truncate">
            {policyLabel(request.policyName, request.policyId)}
          </Type>
        ),
      },
      {
        key: "target",
        header: "Target",
        width: "1.4fr",
        render: (request) => <TargetCell target={request.target} />,
      },
      {
        key: "created",
        header: "Requested",
        width: "170px",
        render: (request) => (
          <Type small muted>
            {formatDate(request.createdAt)}
          </Type>
        ),
      },
      {
        key: "grant",
        header: "Grant bypass",
        width: "360px",
        render: (request) => (
          <GrantControls
            request={request}
            roles={roles}
            isPending={decide.isPending}
            onApprove={(grantType, roleSlugs) =>
              decide.mutate({
                request: {
                  decideRequestRequestBody: {
                    id: request.id,
                    status: "approved",
                    grantType,
                    roleSlugs,
                  },
                },
              })
            }
            onDeny={() =>
              decide.mutate({
                request: {
                  decideRequestRequestBody: {
                    id: request.id,
                    status: "denied",
                  },
                },
              })
            }
          />
        ),
      },
    ],
    [decide, roles],
  );

  const bypassColumns = useMemo<Column<PolicyBypassGrant>[]>(
    () => [
      {
        key: "policy",
        header: "Policy",
        width: "1fr",
        render: (grant) => (
          <Type className="truncate">
            {policyLabel(grant.policyName, grant.policyId)}
          </Type>
        ),
      },
      {
        key: "target",
        header: "Target",
        width: "1.4fr",
        render: (grant) => <TargetCell target={grant.target} />,
      },
      {
        key: "principal",
        header: "Principal",
        width: "1.2fr",
        render: (grant) => (
          <Identity
            primary={principalLabel(grant, roles)}
            secondary={grant.principalUrn}
          />
        ),
      },
      {
        key: "created",
        header: "Granted",
        width: "170px",
        render: (grant) => (
          <Type small muted>
            {formatDate(grant.createdAt)}
          </Type>
        ),
      },
      {
        key: "actions",
        header: "",
        width: "110px",
        render: (grant) => (
          <Button
            size="sm"
            variant="secondary"
            disabled={revoke.isPending}
            onClick={() =>
              revoke.mutate({
                request: {
                  revokeBypassRequestBody: {
                    grantId: grant.id,
                  },
                },
              })
            }
          >
            Revoke
          </Button>
        ),
      },
    ],
    [revoke, roles],
  );

  if (isLoadingRequests || isLoadingBypasses) {
    return <SkeletonTable />;
  }

  return (
    <div className="space-y-8">
      <PolicyAccessSection
        title="Pending requests"
        description="Requests waiting for a bypass decision."
      >
        <Table columns={requestColumns}>
          <Table.Header columns={requestColumns} />
          {requests.length === 0 ? (
            <Table.NoResultsMessage>
              No pending policy access requests.
            </Table.NoResultsMessage>
          ) : (
            <Table.Body
              columns={requestColumns}
              data={requests}
              rowKey={(request) => request.id}
            />
          )}
        </Table>
      </PolicyAccessSection>

      <PolicyAccessSection
        title="Active bypasses"
        description="Current risk-policy bypass grants that can be revoked."
      >
        <Table columns={bypassColumns}>
          <Table.Header columns={bypassColumns} />
          {bypasses.length === 0 ? (
            <Table.NoResultsMessage>
              No active policy bypasses.
            </Table.NoResultsMessage>
          ) : (
            <Table.Body
              columns={bypassColumns}
              data={bypasses}
              rowKey={(grant) => grant.id}
            />
          )}
        </Table>
      </PolicyAccessSection>
    </div>
  );
}

function PolicyAccessSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div>
        <Type variant="subheading">{title}</Type>
        <Type small muted>
          {description}
        </Type>
      </div>
      {children}
    </section>
  );
}

function GrantControls({
  request,
  roles,
  isPending,
  onApprove,
  onDeny,
}: {
  request: PolicyAccessRequest;
  roles: Role[];
  isPending: boolean;
  onApprove: (grantType: GrantType, roleSlugs?: string[]) => void;
  onDeny: () => void;
}) {
  const [grantType, setGrantType] = useState<GrantType>("requester");
  const [selectedRoleSlugs, setSelectedRoleSlugs] = useState<string[]>([]);
  const isSelectedRolesGrant = grantType === "roles";
  const needsRequester =
    grantType === "requester" || grantType === "requester_roles";
  const canApprove =
    !isPending &&
    (!needsRequester || Boolean(request.requesterUserId)) &&
    (!isSelectedRolesGrant || selectedRoleSlugs.length > 0);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={grantType}
          onValueChange={(value) => setGrantType(value as GrantType)}
        >
          <SelectTrigger className="h-8 w-44">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="requester">Requester</SelectItem>
            <SelectItem value="requester_roles">Requester roles</SelectItem>
            <SelectItem value="roles">Selected roles</SelectItem>
          </SelectContent>
        </Select>
        <Button
          size="sm"
          disabled={!canApprove}
          onClick={() =>
            onApprove(
              grantType,
              isSelectedRolesGrant ? selectedRoleSlugs : undefined,
            )
          }
        >
          Approve
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={isPending}
          onClick={onDeny}
        >
          Deny
        </Button>
      </div>
      {isSelectedRolesGrant ? (
        <RolePicker
          roles={roles}
          selectedRoleSlugs={selectedRoleSlugs}
          onChange={setSelectedRoleSlugs}
        />
      ) : null}
    </div>
  );
}

function RolePicker({
  roles,
  selectedRoleSlugs,
  onChange,
}: {
  roles: Role[];
  selectedRoleSlugs: string[];
  onChange: (roleSlugs: string[]) => void;
}) {
  if (roles.length === 0) {
    return (
      <Type small muted>
        No roles available.
      </Type>
    );
  }

  return (
    <div className="grid max-h-28 gap-1 overflow-y-auto rounded-md border p-2">
      {roles.map((role) => {
        const checked = selectedRoleSlugs.includes(role.slug);
        return (
          <label
            key={role.id}
            className="flex cursor-pointer items-center gap-2 text-sm"
          >
            <Checkbox
              checked={checked}
              onCheckedChange={(value) =>
                onChange(
                  toggleRole(selectedRoleSlugs, role.slug, value === true),
                )
              }
            />
            <span className="truncate">{role.name}</span>
          </label>
        );
      })}
    </div>
  );
}

function Identity({
  primary,
  secondary,
}: {
  primary: string;
  secondary?: string | undefined;
}) {
  return (
    <div className="min-w-0">
      <Type className="truncate">{primary}</Type>
      {secondary && secondary !== primary ? (
        <Type small muted mono className="truncate">
          {secondary}
        </Type>
      ) : null}
    </div>
  );
}

function TargetCell({ target }: { target: PolicyAccessTarget }) {
  return (
    <div className="min-w-0">
      <Type className={cn("truncate", target.kind ? "font-medium" : undefined)}>
        {targetLabel(target)}
      </Type>
      {target.kind ? (
        <Type small muted mono className="truncate">
          {formatTargetKind(target.kind)}
        </Type>
      ) : null}
    </div>
  );
}

function policyLabel(name: string | undefined, id: string): string {
  return name && name.length > 0 ? name : id;
}

function targetLabel(target: PolicyAccessTarget): string {
  if (target.label.length > 0) {
    return target.label;
  }
  if (target.key.length > 0) {
    return target.key;
  }
  return "Whole policy";
}

function formatTargetKind(kind: string): string {
  return kind.replace(/_/g, " ");
}

function principalLabel(grant: PolicyBypassGrant, roles: Role[]): string {
  const principalParts = grant.principalUrn.split(":");
  const principalID =
    principalParts[principalParts.length - 1] ?? grant.principalUrn;
  const matchingRole = roles.find((role) => {
    return role.id === principalID || role.slug === principalID;
  });

  if (matchingRole) {
    return matchingRole.name;
  }
  if (grant.principalType === "user") {
    return principalID;
  }
  return `${grant.principalType}: ${principalID}`;
}

function formatDate(date: Date): string {
  return date.toLocaleString();
}

function toggleRole(
  selectedRoleSlugs: string[],
  roleSlug: string,
  checked: boolean,
): string[] {
  if (checked) {
    return Array.from(new Set([...selectedRoleSlugs, roleSlug]));
  }
  return selectedRoleSlugs.filter((slug) => slug !== roleSlug);
}
