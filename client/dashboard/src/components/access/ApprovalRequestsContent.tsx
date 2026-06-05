import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
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
import type { RiskPolicyBypassRequest } from "@gram/client/models/components/riskpolicybypassrequest.js";
import {
  invalidateAllRiskListPolicyBypassRequests,
  useRiskListPolicyBypassRequests,
} from "@gram/client/react-query/riskListPolicyBypassRequests.js";
import { useRiskApprovePolicyBypassRequestMutation } from "@gram/client/react-query/riskApprovePolicyBypassRequest.js";
import { useRiskDenyPolicyBypassRequestMutation } from "@gram/client/react-query/riskDenyPolicyBypassRequest.js";
import { useRiskRevokePolicyBypassRequestMutation } from "@gram/client/react-query/riskRevokePolicyBypassRequest.js";
import { Badge, Button, Column, Dialog, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Inbox, Loader2, ShieldCheck } from "lucide-react";
import type React from "react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { formatShortDate } from "./shadow-mcp-utils";

type ReviewAction = "approve" | "deny";

const SERVER_URL_TARGET_DIMENSION = "server_url";

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
  projectId,
  open,
  isSubmitting,
  onOpenChange,
  onApprove,
  onDeny,
}: {
  request: RiskPolicyBypassRequest | null;
  projectId: string;
  open: boolean;
  isSubmitting: boolean;
  onOpenChange: (open: boolean) => void;
  onApprove: () => Promise<void>;
  onDeny: () => Promise<void>;
}) {
  const [action, setAction] = useState<ReviewAction>("approve");

  useEffect(() => {
    if (!request || !open) return;

    setAction("approve");
  }, [request, open]);

  if (!request) return null;

  const canSubmit = projectId.length > 0;
  const submitLabel = action === "approve" ? "Approve request" : "Deny request";

  const submit = async () => {
    try {
      if (action === "approve") {
        await onApprove();
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
          <SheetTitle>Review request</SheetTitle>
          <SheetDescription>
            Decide how this access request should be handled.
          </SheetDescription>
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

          <Type muted small>
            Approving grants bypass access for the requested policy target.
            Denying leaves the policy block in place.
          </Type>
        </div>

        <SheetFooter>
          <Button
            onClick={submit}
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

export function ApprovalRequestsContent({ projectId }: { projectId: string }) {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canAdmin = hasScope("org:admin");
  const [reviewRequest, setReviewRequest] =
    useState<RiskPolicyBypassRequest | null>(null);
  const [rulePendingDelete, setRulePendingDelete] =
    useState<RiskPolicyBypassRequest | null>(null);

  useEffect(() => {
    setRulePendingDelete(null);
  }, [projectId]);

  const requestsQuery = useRiskListPolicyBypassRequests(
    { status: "requested" },
    undefined,
    { enabled: canAdmin && projectId.length > 0 },
  );
  const rulesQuery = useRiskListPolicyBypassRequests(
    { status: "approved" },
    undefined,
    { enabled: canAdmin && projectId.length > 0 },
  );

  const requests = useMemo(
    () => requestsQuery.data?.requests ?? [],
    [requestsQuery.data?.requests],
  );
  const rules = useMemo(
    () => rulesQuery.data?.requests ?? [],
    [rulesQuery.data?.requests],
  );
  const requestsLoading = requestsQuery.isLoading;
  const requestsError = requestsQuery.error;
  const rulesLoading = rulesQuery.isLoading;
  const rulesError = rulesQuery.error;

  const approveRequest = useRiskApprovePolicyBypassRequestMutation();
  const denyRequest = useRiskDenyPolicyBypassRequestMutation();
  const revokeRequest = useRiskRevokePolicyBypassRequestMutation();
  const isReviewSubmitting = approveRequest.isPending || denyRequest.isPending;

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
        request: { riskIDRequestBody: { id: rulePendingDelete.id } },
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
      key: "requester",
      header: "Requester",
      width: "1.25fr",
      render: (rule) => (
        <ServerCell
          name={getPolicyBypassRequesterLabel(rule)}
          detail={getPolicyBypassRequesterDetail(rule)}
        />
      ),
    },
    {
      key: "status",
      header: "Status",
      width: "0.5fr",
      render: (rule) => <RequestStatusBadge status={rule.status} />,
    },
    {
      key: "principals",
      header: "Principals",
      width: "0.75fr",
      render: (rule) => (
        <Type variant="small">
          {rule.grantedPrincipalUrns.length.toLocaleString()}
        </Type>
      ),
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
      width: "0.5fr",
      render: (rule) => (
        <RequireScope scope="org:admin" level="component">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => setRulePendingDelete(rule)}
          >
            <Button.Text>Revoke</Button.Text>
          </Button>
        </RequireScope>
      ),
    },
  ];

  return (
    <div className="space-y-8">
      <ReviewRequestSheet
        request={reviewRequest}
        projectId={projectId}
        open={!!reviewRequest}
        isSubmitting={isReviewSubmitting}
        onOpenChange={(open) => {
          if (!open) setReviewRequest(null);
        }}
        onApprove={async () => {
          if (!reviewRequest) return;

          await approveRequest.mutateAsync({
            request: {
              riskIDRequestBody: { id: reviewRequest.id },
            },
          });
          await refreshApprovalRequestsData();
          toast.success("Request approved");
          setReviewRequest(null);
        }}
        onDeny={async () => {
          if (!reviewRequest) return;

          await denyRequest.mutateAsync({
            request: {
              riskIDRequestBody: { id: reviewRequest.id },
            },
          });
          await refreshApprovalRequestsData();
          toast.success("Request denied");
          setReviewRequest(null);
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
              onClick={confirmDeleteRule}
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
