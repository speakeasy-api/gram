import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SkeletonTable } from "@/components/ui/skeleton";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { RiskPolicyBypassRequest } from "@gram/client/models/components/riskpolicybypassrequest.js";
import { useMembers } from "@gram/client/react-query/members.js";
import {
  invalidateAllRiskListPolicyBypassRequests,
  useRiskListPolicyBypassRequests,
} from "@gram/client/react-query/riskListPolicyBypassRequests.js";
import { useRiskApprovePolicyBypassRequestMutation } from "@gram/client/react-query/riskApprovePolicyBypassRequest.js";
import { useRiskDenyPolicyBypassRequestMutation } from "@gram/client/react-query/riskDenyPolicyBypassRequest.js";
import { useRiskRevokePolicyBypassRequestMutation } from "@gram/client/react-query/riskRevokePolicyBypassRequest.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { Badge, Button, Column, Dialog, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Inbox, Loader2, ShieldCheck } from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQueryState } from "nuqs";
import { toast } from "sonner";
import { formatShortDate } from "./shadow-mcp-utils";

type ReviewAction = "approve" | "deny";
type ApprovalAudience = "everyone" | "role" | "user";

const SERVER_URL_TARGET_DIMENSION = "server_url";
const ALL_USERS_PRINCIPAL_URN = "user:all";

function userPrincipalUrn(userId: string) {
  return `user:${userId}`;
}

function rolePrincipalUrn(role: Role) {
  const roleKind = role.isSystem ? "global" : "organization";
  return `role:${roleKind}:${role.id}`;
}

function approvalPrincipalUrns({
  audience,
  selectedRoleId,
  selectedUserId,
  roles,
}: {
  audience: ApprovalAudience;
  selectedRoleId: string;
  selectedUserId: string;
  roles: Role[];
}) {
  switch (audience) {
    case "everyone":
      return [ALL_USERS_PRINCIPAL_URN];
    case "role": {
      const role = roles.find((item) => item.id === selectedRoleId);
      return role ? [rolePrincipalUrn(role)] : [];
    }
    case "user":
      return selectedUserId ? [userPrincipalUrn(selectedUserId)] : [];
  }
}

function memberDisplayName(member: AccessMember) {
  if (member.name && member.name !== member.email) {
    return `${member.name} (${member.email})`;
  }
  return member.email;
}

function principalDisplayName(
  principalUrn: string,
  roles: Role[],
  members: AccessMember[],
) {
  if (principalUrn === ALL_USERS_PRINCIPAL_URN) {
    return "Everyone";
  }

  const userPrefix = "user:";
  if (principalUrn.startsWith(userPrefix)) {
    const userId = principalUrn.slice(userPrefix.length);
    const member = members.find((item) => item.id === userId);
    return member ? memberDisplayName(member) : principalUrn;
  }

  const role = roles.find((item) => rolePrincipalUrn(item) === principalUrn);
  return role?.name ?? principalUrn;
}

function principalSummary(
  principalUrns: string[],
  roles: Role[],
  members: AccessMember[],
) {
  if (principalUrns.length === 0) {
    return "None";
  }

  const names = principalUrns.map((principalUrn) =>
    principalDisplayName(principalUrn, roles, members),
  );
  if (names.length <= 2) {
    return names.join(", ");
  }

  return `${names.slice(0, 2).join(", ")} +${names.length - 2}`;
}

function approvalAudienceFromPrincipalUrns(
  principalUrns: string[],
  request: RiskPolicyBypassRequest,
  roles: Role[],
) {
  const principalUrn =
    principalUrns[0] ?? userPrincipalUrn(request.requesterUserId);
  if (principalUrn === ALL_USERS_PRINCIPAL_URN) {
    return {
      audience: "everyone" as const,
      selectedRoleId: "",
      selectedUserId: request.requesterUserId,
    };
  }

  const role = roles.find((item) => rolePrincipalUrn(item) === principalUrn);
  if (role) {
    return {
      audience: "role" as const,
      selectedRoleId: role.id,
      selectedUserId: request.requesterUserId,
    };
  }

  const userPrefix = "user:";
  if (principalUrn.startsWith(userPrefix)) {
    return {
      audience: "user" as const,
      selectedRoleId: "",
      selectedUserId: principalUrn.slice(userPrefix.length),
    };
  }

  return {
    audience: "user" as const,
    selectedRoleId: "",
    selectedUserId: request.requesterUserId,
  };
}

function firstRequesterRoleId(requesterRoleIds: string[], roles: Role[]) {
  return roles.find((role) => requesterRoleIds.includes(role.id))?.id ?? "";
}

function reviewRequestSubmitLabel(
  isEditingAccess: boolean,
  action: ReviewAction,
) {
  if (isEditingAccess) {
    return "Save changes";
  }
  if (action === "approve") {
    return "Approve request";
  }
  return "Deny request";
}

function approvalSuccessMessage(status: RiskPolicyBypassRequest["status"]) {
  if (status === "approved") {
    return "Access updated";
  }
  return "Request approved";
}

function reviewRequestSheetCopy(isEditingAccess: boolean) {
  if (isEditingAccess) {
    return {
      title: "Edit access",
      description: "Change who this bypass applies to.",
      help: "Saving changes updates the principals that receive bypass access for this policy target.",
    };
  }

  return {
    title: "Review request",
    description: "Decide how this access request should be handled.",
    help: "Approving grants bypass access for the requested policy target. Denying leaves the policy block in place.",
  };
}

function TableEmptyState({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="bg-background flex min-h-32 flex-col items-center justify-center gap-1 px-4 py-8 text-center">
      <Type variant="body" className="font-medium">
        {title}
      </Type>
      <Type muted small className="max-w-md">
        {description}
      </Type>
    </div>
  );
}

function ApprovalSectionEmptyState({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  description: React.ReactNode;
  action?: React.ReactNode;
}) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16 text-center">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon className="text-muted-foreground h-6 w-6" />
      </div>
      <Type variant="subheading" className="mb-1">
        {title}
      </Type>
      <Type small muted className="mb-4 max-w-md">
        {description}
      </Type>
      {action}
    </div>
  );
}

function ServerCell({
  name,
  detail,
}: {
  name: string;
  detail?: string | null;
}) {
  return (
    <div className="min-w-0 space-y-1">
      <Type variant="small" className="truncate font-medium">
        {name}
      </Type>
      {detail && (
        <Type
          variant="small"
          className="text-muted-foreground truncate text-xs"
        >
          {detail}
        </Type>
      )}
    </div>
  );
}

function getPolicyBypassRequestStatusLabel(
  status: RiskPolicyBypassRequest["status"],
) {
  switch (status) {
    case "requested":
      return "Requested";
    case "approved":
      return "Approved";
    case "denied":
      return "Denied";
    case "revoked":
      return "Revoked";
  }
}

function RequestStatusBadge({
  status,
}: {
  status: RiskPolicyBypassRequest["status"];
}) {
  const variant =
    status === "approved"
      ? "success"
      : status === "denied" || status === "revoked"
        ? "destructive"
        : "neutral";

  return (
    <Badge variant={variant}>
      <Badge.Text>{getPolicyBypassRequestStatusLabel(status)}</Badge.Text>
    </Badge>
  );
}

function getPolicyBypassTargetURL(request: RiskPolicyBypassRequest) {
  return request.targetDimensions[SERVER_URL_TARGET_DIMENSION];
}

function getPolicyBypassRequestDisplayName(request: RiskPolicyBypassRequest) {
  return (
    request.targetLabel ??
    getPolicyBypassTargetURL(request) ??
    request.targetKey ??
    "Policy target"
  );
}

function getPolicyBypassRequestTargetDetail(request: RiskPolicyBypassRequest) {
  const targetURL = getPolicyBypassTargetURL(request);
  if (targetURL && targetURL !== request.targetLabel) {
    return targetURL;
  }
  if (request.targetKind) {
    return request.targetKind;
  }
  return request.policyId;
}

function getPolicyBypassRequestTargetType(request: RiskPolicyBypassRequest) {
  switch (request.targetKind) {
    case "shadow_mcp_server":
      return "Shadow MCP";
    case "":
    case undefined:
      return "Policy";
    default:
      return request.targetKind;
  }
}

function getPolicyBypassRequesterLabel(request: RiskPolicyBypassRequest) {
  return request.requesterEmail ?? request.requesterUserId ?? "Unknown user";
}

function getPolicyBypassRequesterDetail(request: RiskPolicyBypassRequest) {
  if (request.requesterEmail) {
    return request.requesterUserId;
  }
  return undefined;
}

function ReviewRequestSheet({
  request,
  projectSlug,
  roles,
  members,
  open,
  isSubmitting,
  onOpenChange,
  onApprove,
  onDeny,
}: {
  request: RiskPolicyBypassRequest | null;
  projectSlug: string;
  roles: Role[];
  members: AccessMember[];
  open: boolean;
  isSubmitting: boolean;
  onOpenChange: (open: boolean) => void;
  onApprove: (principalUrns: string[]) => Promise<void>;
  onDeny: () => Promise<void>;
}) {
  const [action, setAction] = useState<ReviewAction>("approve");
  const [approvalAudience, setApprovalAudience] =
    useState<ApprovalAudience>("user");
  const [approvalAudienceDirty, setApprovalAudienceDirty] = useState(false);
  const [selectedRoleId, setSelectedRoleId] = useState("");
  const [selectedUserId, setSelectedUserId] = useState("");
  const requesterRoleIds = useMemo(() => {
    if (!request) return [];
    return (
      members.find((member) => member.id === request.requesterUserId)
        ?.roleIds ?? []
    );
  }, [members, request]);
  const requesterRoleIdSet = useMemo(
    () => new Set(requesterRoleIds),
    [requesterRoleIds],
  );
  const defaultRequesterRoleId = firstRequesterRoleId(requesterRoleIds, roles);

  useEffect(() => {
    if (!request || !open) return;

    const initial = approvalAudienceFromPrincipalUrns(
      request.grantedPrincipalUrns,
      request,
      roles,
    );
    setAction("approve");
    setApprovalAudienceDirty(false);
    setApprovalAudience(initial.audience);
    setSelectedRoleId(initial.selectedRoleId || defaultRequesterRoleId);
    setSelectedUserId(initial.selectedUserId);
  }, [defaultRequesterRoleId, request, roles, open]);

  if (!request) return null;

  const isEditingAccess = request.status === "approved";
  const principalUrns =
    isEditingAccess && !approvalAudienceDirty
      ? request.grantedPrincipalUrns
      : approvalPrincipalUrns({
          audience: approvalAudience,
          selectedRoleId,
          selectedUserId,
          roles,
        });
  const approveReady = action !== "approve" || principalUrns.length > 0;
  const canSubmit = projectSlug.length > 0 && approveReady;
  const submitLabel = reviewRequestSubmitLabel(isEditingAccess, action);
  const sheetCopy = reviewRequestSheetCopy(isEditingAccess);
  const selectApprovalAudience = (audience: ApprovalAudience) => {
    setApprovalAudienceDirty(true);
    setApprovalAudience(audience);
    if (
      audience === "role" &&
      selectedRoleId === "" &&
      defaultRequesterRoleId
    ) {
      setSelectedRoleId(defaultRequesterRoleId);
    }
    if (audience === "user" && selectedUserId === "") {
      setSelectedUserId(request.requesterUserId);
    }
  };

  const submit = async (): Promise<void> => {
    try {
      if (isEditingAccess || action === "approve") {
        await onApprove(principalUrns);
      } else {
        await onDeny();
      }
    } catch {
      toast.error("Request review failed");
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{sheetCopy.title}</SheetTitle>
          <SheetDescription>{sheetCopy.description}</SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4">
          <section className="border-border rounded-md border px-4 py-3">
            <div className="flex items-start justify-between gap-3">
              <ServerCell
                name={getPolicyBypassRequestDisplayName(request)}
                detail={getPolicyBypassRequestTargetDetail(request)}
              />
              <RequestStatusBadge status={request.status} />
            </div>
            <div className="mt-4 grid grid-cols-6 gap-4">
              <div className="col-span-3 min-w-0">
                <Type muted small>
                  Requester
                </Type>
                <Type variant="body" className="mt-1 truncate text-sm">
                  {getPolicyBypassRequesterLabel(request)}
                </Type>
                {getPolicyBypassRequesterDetail(request) && (
                  <Type
                    variant="body"
                    className="text-muted-foreground truncate text-xs"
                  >
                    {getPolicyBypassRequesterDetail(request)}
                  </Type>
                )}
              </div>
              <div className="col-span-2">
                <Type muted small>
                  Updated
                </Type>
                <Type variant="body" className="mt-1 text-sm">
                  {formatShortDate(request.updatedAt)}
                </Type>
              </div>
              <div className="col-span-1">
                <Type muted small>
                  Created
                </Type>
                <Type variant="body" className="mt-1 text-sm">
                  {formatShortDate(request.createdAt)}
                </Type>
              </div>
            </div>
          </section>

          {!isEditingAccess && (
            <RadioGroup
              value={action}
              onValueChange={(value) => setAction(value as ReviewAction)}
              className="border-border grid grid-cols-2 gap-4 rounded-md border p-3"
            >
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                  action === "approve" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="approve" className="mt-1.5" />
                <span>
                  <Badge variant="success">
                    <Badge.Text>Approve</Badge.Text>
                  </Badge>
                  <Type muted small>
                    Allow matching access.
                  </Type>
                </span>
              </label>
              <label
                className={cn(
                  "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                  action === "deny" && "border-border bg-card shadow-xs",
                )}
              >
                <RadioGroupItem value="deny" className="mt-1.5" />
                <span>
                  <Badge variant="destructive">
                    <Badge.Text>Deny</Badge.Text>
                  </Badge>
                  <Type muted small>
                    Reject the request.
                  </Type>
                </span>
              </label>
            </RadioGroup>
          )}

          {(isEditingAccess || action === "approve") && (
            <section className="border-border space-y-3 rounded-md border p-3">
              <Type variant="small" className="font-medium">
                Applies to
              </Type>
              <RadioGroup
                value={approvalAudience}
                onValueChange={(value) =>
                  selectApprovalAudience(value as ApprovalAudience)
                }
                className="space-y-2"
              >
                <label
                  className={cn(
                    "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                    approvalAudience === "everyone" &&
                      "border-border bg-card shadow-xs",
                  )}
                >
                  <RadioGroupItem value="everyone" className="mt-1" />
                  <span>
                    <Type variant="small" className="font-medium">
                      Everyone
                    </Type>
                    <Type muted small>
                      All users in this organization.
                    </Type>
                  </span>
                </label>

                <label
                  className={cn(
                    "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                    approvalAudience === "role" &&
                      "border-border bg-card shadow-xs",
                  )}
                >
                  <RadioGroupItem value="role" className="mt-1" />
                  <span className="min-w-0 flex-1 space-y-2">
                    <span>
                      <Type variant="small" className="font-medium">
                        Role
                      </Type>
                      <Type muted small>
                        Users assigned to one role.
                      </Type>
                    </span>
                    <Select
                      value={selectedRoleId}
                      onValueChange={(value) => {
                        selectApprovalAudience("role");
                        setSelectedRoleId(value);
                      }}
                      disabled={roles.length === 0}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="Select role" />
                      </SelectTrigger>
                      <SelectContent>
                        {roles.map((role) => (
                          <SelectItem key={role.id} value={role.id}>
                            <span className="flex items-center gap-2">
                              <span>{role.name}</span>
                              {requesterRoleIdSet.has(role.id) && (
                                <Badge variant="neutral">
                                  <Badge.Text>Current</Badge.Text>
                                </Badge>
                              )}
                            </span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </span>
                </label>

                <label
                  className={cn(
                    "flex cursor-pointer items-start gap-3 rounded-sm border border-transparent px-3 py-2.5 transition-colors",
                    approvalAudience === "user" &&
                      "border-border bg-card shadow-xs",
                  )}
                >
                  <RadioGroupItem value="user" className="mt-1" />
                  <span className="min-w-0 flex-1 space-y-2">
                    <span>
                      <Type variant="small" className="font-medium">
                        User
                      </Type>
                      <Type muted small>
                        One organization member.
                      </Type>
                    </span>
                    <Select
                      value={selectedUserId}
                      onValueChange={(value) => {
                        selectApprovalAudience("user");
                        setSelectedUserId(value);
                      }}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="Select user" />
                      </SelectTrigger>
                      <SelectContent>
                        {members.map((member) => (
                          <SelectItem key={member.id} value={member.id}>
                            {memberDisplayName(member)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </span>
                </label>
              </RadioGroup>
            </section>
          )}

          <Type muted small>
            {sheetCopy.help}
          </Type>
        </div>

        <SheetFooter>
          <Button
            onClick={() => {
              void submit();
            }}
            disabled={!canSubmit || isSubmitting}
            className="w-full"
          >
            <Button.Text>{submitLabel}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

export function ApprovalRequestsContent({
  projectSlug,
}: {
  projectSlug: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canAdmin = hasScope("org:admin");
  const [reviewRequest, setReviewRequest] =
    useState<RiskPolicyBypassRequest | null>(null);
  const [rulePendingDelete, setRulePendingDelete] =
    useState<RiskPolicyBypassRequest | null>(null);

  useEffect(() => {
    setRulePendingDelete(null);
  }, [projectSlug]);

  const requestsQuery = useRiskListPolicyBypassRequests(
    { status: "requested", gramProject: projectSlug },
    undefined,
    { enabled: canAdmin && projectSlug.length > 0 },
  );
  const rulesQuery = useRiskListPolicyBypassRequests(
    { status: "approved", gramProject: projectSlug },
    undefined,
    { enabled: canAdmin && projectSlug.length > 0 },
  );
  const rolesQuery = useRoles(undefined, undefined, { enabled: canAdmin });
  const membersQuery = useMembers(undefined, undefined, { enabled: canAdmin });

  const requests = useMemo(
    () => requestsQuery.data?.requests ?? [],
    [requestsQuery.data?.requests],
  );
  const rules = useMemo(
    () => rulesQuery.data?.requests ?? [],
    [rulesQuery.data?.requests],
  );
  const roles = useMemo(() => rolesQuery.data?.roles ?? [], [rolesQuery.data]);
  const members = useMemo(
    () => membersQuery.data?.members ?? [],
    [membersQuery.data],
  );
  const requestsLoading = requestsQuery.isLoading;
  const requestsError = requestsQuery.error;
  const rulesLoading = rulesQuery.isLoading;
  const rulesError = rulesQuery.error;

  const approveRequest = useRiskApprovePolicyBypassRequestMutation();
  const denyRequest = useRiskDenyPolicyBypassRequestMutation();
  const revokeRequest = useRiskRevokePolicyBypassRequestMutation();
  const isReviewSubmitting = approveRequest.isPending || denyRequest.isPending;

  // The command palette deep-links to pending requests because they do not have
  // per-request routes.
  const [reviewParam, setReviewParam] = useQueryState("review");
  const openedReviewRef = useRef<string | null>(null);
  useEffect(() => {
    if (!reviewParam || requestsLoading) return;
    if (openedReviewRef.current === reviewParam) return;

    const request = requests.find((item) => item.id === reviewParam);
    if (request) {
      openedReviewRef.current = reviewParam;
      setReviewRequest(request);
    }
  }, [reviewParam, requestsLoading, requests]);

  const closeReviewSheet = useCallback(() => {
    setReviewRequest(null);
    openedReviewRef.current = null;
    void setReviewParam(null);
  }, [setReviewParam]);

  const refreshApprovalRequestsData = async () => {
    await invalidateAllRiskListPolicyBypassRequests(queryClient);
  };

  const closeDeleteRuleDialog = () => {
    if (revokeRequest.isPending) return;
    setRulePendingDelete(null);
  };

  const confirmDeleteRule = async () => {
    if (!rulePendingDelete || revokeRequest.isPending) return;

    try {
      await revokeRequest.mutateAsync({
        request: {
          gramProject: projectSlug,
          riskIDRequestBody: { id: rulePendingDelete.id },
        },
      });
      await refreshApprovalRequestsData();
      toast.success("Access revoked");
      setRulePendingDelete(null);
    } catch {
      toast.error("Access revoke failed");
    }
  };

  const requestColumns: Column<RiskPolicyBypassRequest>[] = [
    {
      key: "server",
      header: "Server",
      width: "1.5fr",
      render: (request) => (
        <ServerCell
          name={getPolicyBypassRequestDisplayName(request)}
          detail={getPolicyBypassRequestTargetDetail(request)}
        />
      ),
    },
    {
      key: "resourceType",
      header: "Type",
      width: "0.75fr",
      render: (request) => (
        <Badge variant="neutral">
          <Badge.Text>{getPolicyBypassRequestTargetType(request)}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "requester",
      header: "Requester",
      width: "1.25fr",
      render: (request) => (
        <ServerCell
          name={getPolicyBypassRequesterLabel(request)}
          detail={getPolicyBypassRequesterDetail(request)}
        />
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "0.5fr",
      render: (request) => <RequestStatusBadge status={request.status} />,
    },
    {
      key: "updated",
      header: "Updated",
      width: "0.75fr",
      render: (request) => (
        <Type variant="small">{formatShortDate(request.updatedAt)}</Type>
      ),
    },
  ];
  const pendingRequestColumns: Column<RiskPolicyBypassRequest>[] = [
    ...requestColumns,
    {
      key: "actions",
      header: "",
      width: "0.5fr",
      render: (request) => (
        <RequireScope scope="org:admin" level="component">
          <Button size="sm" onClick={() => setReviewRequest(request)}>
            <Button.Text>Review</Button.Text>
          </Button>
        </RequireScope>
      ),
    },
  ];

  const ruleColumns: Column<RiskPolicyBypassRequest>[] = [
    {
      key: "server",
      header: "Server",
      width: "1.5fr",
      render: (rule) => (
        <ServerCell
          name={getPolicyBypassRequestDisplayName(rule)}
          detail={getPolicyBypassRequestTargetDetail(rule)}
        />
      ),
    },
    {
      key: "resourceType",
      header: "Type",
      width: "0.75fr",
      render: (rule) => (
        <Badge variant="neutral">
          <Badge.Text>{getPolicyBypassRequestTargetType(rule)}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "appliesTo",
      header: "Applies to",
      width: "1.5fr",
      render: (rule) => (
        <Type variant="small" className="truncate">
          {principalSummary(rule.grantedPrincipalUrns, roles, members)}
        </Type>
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "0.5fr",
      render: (rule) => <RequestStatusBadge status={rule.status} />,
    },
    {
      key: "updated",
      header: "Updated",
      width: "0.75fr",
      render: (rule) => (
        <Type variant="small">{formatShortDate(rule.updatedAt)}</Type>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.75fr",
      render: (rule) => (
        <RequireScope scope="org:admin" level="component">
          <div className="flex justify-end gap-2">
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setReviewRequest(rule)}
            >
              <Button.Text>Edit</Button.Text>
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setRulePendingDelete(rule)}
            >
              <Button.Text>Revoke</Button.Text>
            </Button>
          </div>
        </RequireScope>
      ),
    },
  ];

  return (
    <div className="space-y-8">
      <ReviewRequestSheet
        request={reviewRequest}
        projectSlug={projectSlug}
        roles={roles}
        members={members}
        open={!!reviewRequest}
        isSubmitting={isReviewSubmitting}
        onOpenChange={(open) => {
          if (!open) closeReviewSheet();
        }}
        onApprove={async (principalUrns) => {
          if (!reviewRequest) return;

          await approveRequest.mutateAsync({
            request: {
              gramProject: projectSlug,
              riskPolicyBypassApprovalRequestBody: {
                id: reviewRequest.id,
                grantedPrincipalUrns: principalUrns,
              },
            },
          });
          await refreshApprovalRequestsData();
          toast.success(approvalSuccessMessage(reviewRequest.status));
          closeReviewSheet();
        }}
        onDeny={async () => {
          if (!reviewRequest) return;

          await denyRequest.mutateAsync({
            request: {
              gramProject: projectSlug,
              riskIDRequestBody: { id: reviewRequest.id },
            },
          });
          await refreshApprovalRequestsData();
          toast.success("Request denied");
          closeReviewSheet();
        }}
      />

      {canAdmin && (
        <section className="space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <Heading variant="h5">Approval Requests</Heading>
              <Type muted small className="mt-1">
                Review access requests users created after a policy block.
              </Type>
            </div>
          </div>

          {requestsLoading ? (
            <SkeletonTable />
          ) : requestsError ? (
            <TableEmptyState
              title="Requests could not be loaded"
              description="Refresh the page or try again later."
            />
          ) : requests.length === 0 ? (
            <ApprovalSectionEmptyState
              icon={Inbox}
              title="No approval requests"
              description="Requests will appear here when users ask for access after a policy block."
            />
          ) : (
            <div className="overflow-hidden rounded-lg border">
              <Table
                columns={pendingRequestColumns}
                data={requests}
                rowKey={(row) => row.id}
                className="[&_thead]:bg-background max-h-128 overflow-y-auto rounded-none border-0 [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
              />
            </div>
          )}
        </section>
      )}

      <section className="space-y-4">
        <div className="flex items-start justify-between gap-4">
          <div>
            <Heading variant="h5">Access Rules</Heading>
            <Type muted small className="mt-1">
              Manage approved policy bypass access.
            </Type>
          </div>
        </div>

        {rulesLoading ? (
          <SkeletonTable />
        ) : rulesError ? (
          <TableEmptyState
            title="Access Rules could not be loaded"
            description="Refresh the page or try again later."
          />
        ) : rules.length === 0 ? (
          <ApprovalSectionEmptyState
            icon={ShieldCheck}
            title="No access rules"
            description="Approved policy bypass requests will appear here."
          />
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table
              columns={ruleColumns}
              data={rules}
              rowKey={(row) => row.id}
              className="[&_thead]:bg-background max-h-128 overflow-y-auto rounded-none border-0 [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
            />
          </div>
        )}
      </section>

      <Dialog
        open={rulePendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) closeDeleteRuleDialog();
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke access</Dialog.Title>
          </Dialog.Header>
          <Type variant="small">
            This removes the bypass grant for{" "}
            <code className="bg-muted rounded px-1 py-0.5 font-mono font-bold">
              {rulePendingDelete
                ? getPolicyBypassRequestDisplayName(rulePendingDelete)
                : "this access rule"}
            </code>
            ?
          </Type>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={closeDeleteRuleDialog}
              disabled={revokeRequest.isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              onClick={() => {
                void confirmDeleteRule();
              }}
              disabled={revokeRequest.isPending}
            >
              {revokeRequest.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Revoking</Button.Text>
                </>
              ) : (
                <Button.Text>Revoke</Button.Text>
              )}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}
