import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
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
import { Input } from "@/components/moon/input";
import { Textarea } from "@/components/moon/textarea";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { useSdkClient } from "@/contexts/Sdk";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPAccessRule } from "@gram/client/models/components/shadowmcpaccessrule.js";
import type { ShadowMCPApprovalRequest } from "@gram/client/models/components/shadowmcpapprovalrequest.js";
import { useApproveShadowMCPApprovalRequestMutation } from "@gram/client/react-query/approveShadowMCPApprovalRequest.js";
import { useCreateShadowMCPAccessRuleMutation } from "@gram/client/react-query/createShadowMCPAccessRule.js";
import { useDeleteShadowMCPAccessRuleMutation } from "@gram/client/react-query/deleteShadowMCPAccessRule.js";
import { useDenyShadowMCPApprovalRequestMutation } from "@gram/client/react-query/denyShadowMCPApprovalRequest.js";
import { invalidateAllShadowMCPAccessRules } from "@gram/client/react-query/shadowMCPAccessRules.js";
import { invalidateAllShadowMCPApprovalRequests } from "@gram/client/react-query/shadowMCPApprovalRequests.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useUpdateShadowMCPAccessRuleMutation } from "@gram/client/react-query/updateShadowMCPAccessRule.js";
import {
  Badge,
  Button,
  Column,
  Dialog,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Table,
  Tooltip,
  TooltipContent,
  TooltipPortal,
  TooltipTrigger,
} from "@speakeasy-api/moonshine";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { Ellipsis, Inbox, Loader2, Plus, ShieldCheck } from "lucide-react";
import type React from "react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  formatShortDate,
  getAccessScopeLabel,
  getDefaultMatchBreadth,
  getDispositionLabel,
  getMatchBreadthLabel,
  getMatchValue,
  normalizeRuleMatchBreadth,
  getRequesterDetail,
  getRequesterLabel,
  getRequestDisplayName,
  getRequestServerDetail,
  getRequestStatusLabel,
  getResourceTypeLabel,
  getRuleDisplayName,
  getRuleServerDetail,
  type ShadowMCPDisposition,
  type ShadowMCPMatchBreadth,
} from "./shadow-mcp-utils";

type RuleDispositionFilter = "allowed" | "denied" | "all";
type ReviewAction = "approve" | "deny";

const APPROVAL_REQUESTS_PAGE_SIZE = 100;
const APPROVAL_REQUESTS_QUERY_KEY = ["approval-requests", "requests"];
const APPROVAL_REQUEST_RULES_QUERY_KEY = ["approval-requests", "access-rules"];
const ACCESS_RULE_BLOCKING_POLICY_REQUIREMENTS = [
  {
    source: "shadow_mcp",
    label: "Shadow MCP",
  },
] as const;
const CREATE_BLOCKING_POLICY_MESSAGE =
  "Create a blocking Shadow MCP policy before adding access rules.";
const DEFAULT_ACCESS_RULES_EMPTY_STATE_DESCRIPTION =
  "Create a rule manually or approve a request to allow or deny matching resources.";

type AccessRuleCreateAvailability =
  | { status: "available" }
  | { status: "checking"; reason: string }
  | { status: "missing_blocking_policy"; reason: string };

const MATCH_BREADTH_OPTIONS: {
  value: ShadowMCPMatchBreadth;
  label: string;
}[] = [
  { value: "full_url", label: "Full URL" },
  { value: "url_host", label: "URL host" },
];

function getAccessRuleCreateAvailability({
  canCreateAccessRules,
  isLoadingPolicies,
}: {
  canCreateAccessRules: boolean;
  isLoadingPolicies: boolean;
}): AccessRuleCreateAvailability {
  if (isLoadingPolicies) {
    return {
      status: "checking",
      reason: "Checking Shadow MCP policies...",
    };
  }

  if (!canCreateAccessRules) {
    return {
      status: "missing_blocking_policy",
      reason: CREATE_BLOCKING_POLICY_MESSAGE,
    };
  }

  return { status: "available" };
}

function policyEnablesAccessRules(policy: RiskPolicy) {
  return (
    policy.enabled &&
    policy.action === "block" &&
    ACCESS_RULE_BLOCKING_POLICY_REQUIREMENTS.some((requirement) =>
      policy.sources.includes(requirement.source),
    )
  );
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

function getAccessRulesEmptyDescription(
  availability: AccessRuleCreateAvailability,
) {
  if (availability.status === "missing_blocking_policy") {
    const policyLabel = ACCESS_RULE_BLOCKING_POLICY_REQUIREMENTS.map(
      (requirement) => requirement.label,
    ).join(", ");

    return `Create a blocking risk policy for ${policyLabel} servers before adding access rules.`;
  }

  if (availability.status === "checking") {
    return availability.reason;
  }

  return DEFAULT_ACCESS_RULES_EMPTY_STATE_DESCRIPTION;
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

function RequestStatusBadge({
  status,
}: {
  status: ShadowMCPApprovalRequest["status"];
}) {
  const variant =
    status === "approved"
      ? "success"
      : status === "denied"
        ? "destructive"
        : "neutral";

  return (
    <Badge variant={variant}>
      <Badge.Text>{getRequestStatusLabel(status)}</Badge.Text>
    </Badge>
  );
}

function RuleDispositionBadge({
  disposition,
}: {
  disposition: ShadowMCPAccessRule["disposition"];
}) {
  return (
    <Badge variant={disposition === "allowed" ? "success" : "destructive"}>
      <Badge.Text>{getDispositionLabel(disposition)}</Badge.Text>
    </Badge>
  );
}

function AccessRuleTableCell({
  inactive,
  children,
}: {
  inactive: boolean;
  children: React.ReactNode;
}) {
  return (
    <div
      data-inactive-rule-cell={inactive ? "true" : undefined}
      className={cn("min-w-0", inactive && "opacity-50")}
    >
      {children}
    </div>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="block space-y-2">
      <Type variant="body" className="text-sm font-medium">
        {label}
      </Type>
      {children}
    </label>
  );
}

function projectName(
  projects: { id: string; name: string }[] | undefined,
  projectId?: string | null,
) {
  if (!projectId) return "Project";
  return (
    projects?.find((project) => project.id === projectId)?.name ?? "Project"
  );
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
  request: ShadowMCPApprovalRequest | null;
  projectId: string;
  open: boolean;
  isSubmitting: boolean;
  onOpenChange: (open: boolean) => void;
  onApprove: (input: {
    displayName: string;
    projectIds: string[];
    matchBreadth: ShadowMCPMatchBreadth;
    matchValue: string;
    reason?: string;
  }) => Promise<void>;
  onDeny: (input: {
    createDenyRule: boolean;
    projectIds: string[];
    displayName?: string;
    matchBreadth?: ShadowMCPMatchBreadth;
    matchValue?: string;
    reason?: string;
  }) => Promise<void>;
}) {
  const [action, setAction] = useState<ReviewAction>("approve");
  const [displayName, setDisplayName] = useState("");
  const [matchBreadth, setMatchBreadth] =
    useState<ShadowMCPMatchBreadth>("full_url");
  const [matchValue, setMatchValue] = useState("");
  const [reason, setReason] = useState("");
  const [createDenyRule, setCreateDenyRule] = useState(false);

  useEffect(() => {
    if (!request || !open) return;

    const nextMatchBreadth = getDefaultMatchBreadth(request);
    setAction("approve");
    setDisplayName(getRequestDisplayName(request));
    setMatchBreadth(nextMatchBreadth);
    setMatchValue(getMatchValue(request, nextMatchBreadth));
    setReason("");
    setCreateDenyRule(false);
  }, [request, open]);

  if (!request) return null;

  const projectIds = [projectId];
  const requiresRuleFields = action === "approve" || createDenyRule;
  const canSubmit =
    projectId.length > 0 &&
    (!requiresRuleFields ||
      (displayName.trim().length > 0 && matchValue.trim().length > 0));
  const submitLabel =
    action === "approve"
      ? "Approve and create rule"
      : createDenyRule
        ? "Deny and create rule"
        : "Deny request";

  const submit = async () => {
    const trimmedReason = reason.trim() || undefined;

    try {
      if (action === "approve") {
        await onApprove({
          displayName: displayName.trim(),
          projectIds,
          matchBreadth,
          matchValue: matchValue.trim(),
          reason: trimmedReason,
        });
      } else {
        await onDeny({
          createDenyRule,
          projectIds,
          displayName: displayName.trim(),
          matchBreadth,
          matchValue: matchValue.trim(),
          reason: trimmedReason,
        });
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
                name={getRequestDisplayName(request)}
                detail={getRequestServerDetail(request)}
              />
              <RequestStatusBadge status={request.status} />
            </div>
            <div className="mt-4 grid grid-cols-6 gap-4">
              <div className="col-span-3 min-w-0">
                <Type muted small>
                  Requester
                </Type>
                <Type variant="body" className="mt-1 truncate text-sm">
                  {getRequesterLabel(request)}
                </Type>
                {getRequesterDetail(request) && (
                  <Type
                    variant="body"
                    className="text-muted-foreground truncate text-xs"
                  >
                    {getRequesterDetail(request)}
                  </Type>
                )}
              </div>
              <div className="col-span-2">
                <Type muted small>
                  Last blocked
                </Type>
                <Type variant="body" className="mt-1 text-sm">
                  {formatShortDate(request.lastBlockedAt)}
                </Type>
              </div>
              <div className="col-span-1">
                <Type muted small>
                  Blocked
                </Type>
                <Type variant="body" className="mt-1 text-sm">
                  {request.blockedCount.toLocaleString()}{" "}
                  {request.blockedCount === 1 ? "time" : "times"}
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

          <section className="border-border space-y-4 rounded-md border px-4 py-4">
            <div>
              <Type variant="body" className="text-sm font-medium">
                Rule
              </Type>
              <Type muted small>
                {action === "approve"
                  ? "Allow rule details"
                  : "Deny rule details"}
              </Type>
            </div>

            <Field label="Rule name">
              <Input
                value={displayName}
                onChange={(event) => setDisplayName(event.target.value)}
              />
            </Field>

            <Field label="Match">
              <Select
                value={matchBreadth}
                onValueChange={(value) => {
                  const nextBreadth = value as ShadowMCPMatchBreadth;
                  setMatchBreadth(nextBreadth);
                  setMatchValue(getMatchValue(request, nextBreadth));
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {MATCH_BREADTH_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Match value">
              <Input
                value={matchValue}
                onChange={(event) => setMatchValue(event.target.value)}
              />
            </Field>

            <Field label="Reason">
              <Textarea
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                placeholder="Optional"
              />
            </Field>

            {action === "deny" && (
              <label className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={createDenyRule}
                  onCheckedChange={(checked) =>
                    setCreateDenyRule(checked === true)
                  }
                />
                <span>Create deny rule</span>
              </label>
            )}
          </section>
        </div>

        <SheetFooter>
          <Button
            onClick={() => void submit()}
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

function AccessRuleSheet({
  rule,
  projectId,
  open,
  isSubmitting,
  onOpenChange,
  onSubmit,
}: {
  rule: ShadowMCPAccessRule | null;
  projectId: string;
  open: boolean;
  isSubmitting: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    displayName: string;
    disposition: ShadowMCPDisposition;
    projectId?: string;
    projectIds: string[];
    matchBreadth: ShadowMCPMatchBreadth;
    matchValue: string;
    reason?: string;
  }) => Promise<void>;
}) {
  const [disposition, setDisposition] =
    useState<ShadowMCPDisposition>("allowed");
  const [displayName, setDisplayName] = useState("");
  const [matchBreadth, setMatchBreadth] =
    useState<ShadowMCPMatchBreadth>("full_url");
  const [matchValue, setMatchValue] = useState("");
  const [reason, setReason] = useState("");

  useEffect(() => {
    if (!open) return;

    if (rule) {
      setDisposition(rule.disposition);
      setDisplayName(rule.displayName);
      setMatchBreadth(normalizeRuleMatchBreadth(rule.matchBreadth));
      setMatchValue(rule.matchValue);
      setReason(rule.reason ?? "");
      return;
    }

    setDisposition("allowed");
    setDisplayName("");
    setMatchBreadth("full_url");
    setMatchValue("");
    setReason("");
  }, [rule, open]);

  const projectIds = [projectId];
  const canSubmit =
    displayName.trim().length > 0 &&
    matchValue.trim().length > 0 &&
    projectId.length > 0;

  const submit = async () => {
    try {
      await onSubmit({
        displayName: displayName.trim(),
        disposition,
        projectId,
        projectIds,
        matchBreadth,
        matchValue: matchValue.trim(),
        reason: reason.trim() || undefined,
      });
    } catch {
      toast.error(
        rule ? "Access Rule update failed" : "Access Rule creation failed",
      );
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>
            {rule ? "Edit Access Rule" : "Add Access Rule"}
          </SheetTitle>
          <SheetDescription>
            Configure an allow or deny decision for matching requests.
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4">
          <RadioGroup
            value={disposition}
            onValueChange={(value) =>
              setDisposition(value as ShadowMCPDisposition)
            }
            className="border-border bg-muted/20 grid grid-cols-2 gap-1 rounded-md border p-1"
          >
            <label
              className={cn(
                "flex cursor-pointer items-start gap-3 rounded-sm px-3 py-2.5 transition-colors",
                disposition === "allowed" && "bg-background shadow-xs",
              )}
            >
              <RadioGroupItem value="allowed" className="mt-0.5" />
              <span>
                <Badge variant="success">
                  <Badge.Text>Allow</Badge.Text>
                </Badge>
                <Type muted small>
                  Allow matching calls.
                </Type>
              </span>
            </label>
            <label
              className={cn(
                "flex cursor-pointer items-start gap-3 rounded-sm px-3 py-2.5 transition-colors",
                disposition === "denied" && "bg-background shadow-xs",
              )}
            >
              <RadioGroupItem value="denied" className="mt-0.5" />
              <span>
                <Badge variant="destructive">
                  <Badge.Text>Deny</Badge.Text>
                </Badge>
                <Type muted small>
                  Block matching calls.
                </Type>
              </span>
            </label>
          </RadioGroup>

          <section className="border-border space-y-4 rounded-md border px-4 py-4">
            <div>
              <Type variant="body" className="text-sm font-medium">
                Rule
              </Type>
              <Type muted small>
                {disposition === "allowed"
                  ? "Allow rule details"
                  : "Deny rule details"}
              </Type>
            </div>

            <Field label="Rule name">
              <Input
                value={displayName}
                onChange={(event) => setDisplayName(event.target.value)}
                placeholder="Datadog"
              />
            </Field>

            <Field label="Match">
              <Select
                value={matchBreadth}
                onValueChange={(value) =>
                  setMatchBreadth(value as ShadowMCPMatchBreadth)
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {MATCH_BREADTH_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="Match value">
              <Input
                value={matchValue}
                onChange={(event) => setMatchValue(event.target.value)}
                placeholder="https://example.com/mcp"
              />
            </Field>

            <Field label="Reason">
              <Textarea
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                placeholder="Optional"
              />
            </Field>
          </section>
        </div>

        <SheetFooter>
          <Button
            onClick={() => void submit()}
            disabled={!canSubmit || isSubmitting}
            className="w-full"
          >
            <Button.Text>{rule ? "Save rule" : "Add rule"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function RuleActionsMenu({
  onEdit,
  onDelete,
}: {
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <RequireScope scope="org:admin" level="component">
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className={cn(
              "text-muted-foreground hover:bg-accent hover:text-foreground flex h-8 w-8 cursor-pointer items-center justify-center rounded-md transition-colors",
            )}
          >
            <Ellipsis className="h-4 w-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            onSelect={() => {
              void setTimeout(onEdit, 0);
            }}
          >
            Edit
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={() => {
              void setTimeout(onDelete, 0);
            }}
          >
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </RequireScope>
  );
}

function AddRuleButton({
  disabled,
  disabledReason,
  onClick,
}: {
  disabled?: boolean;
  disabledReason?: string;
  onClick: () => void;
}) {
  const button = (
    <Button disabled={disabled} onClick={disabled ? undefined : onClick}>
      <Button.LeftIcon>
        <Plus className="h-4 w-4" />
      </Button.LeftIcon>
      <Button.Text>Add Rule</Button.Text>
    </Button>
  );

  if (!disabled) {
    return button;
  }

  return (
    <Tooltip delayDuration={0}>
      <TooltipTrigger asChild>
        <div className="inline-flex cursor-not-allowed">{button}</div>
      </TooltipTrigger>
      <TooltipPortal>
        <TooltipContent>{disabledReason}</TooltipContent>
      </TooltipPortal>
    </Tooltip>
  );
}

function ManagePoliciesButton() {
  return (
    <Button variant="primary" asChild>
      <Link to="../risk-policies" relative="path">
        <Button.Text>Manage Policies</Button.Text>
        <Button.RightIcon>
          <Icon name="arrow-right" />
        </Button.RightIcon>
      </Link>
    </Button>
  );
}

export function ApprovalRequestsContent({
  projectId,
}: {
  projectId: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const client = useSdkClient();
  const organization = useOrganization();
  const { hasScope } = useRBAC();
  const canAdmin = hasScope("org:admin");
  const [ruleDispositionFilter, setRuleDispositionFilter] =
    useState<RuleDispositionFilter>("all");
  const [reviewRequest, setReviewRequest] =
    useState<ShadowMCPApprovalRequest | null>(null);
  const [isRuleSheetOpen, setIsRuleSheetOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<ShadowMCPAccessRule | null>(
    null,
  );
  const [rulePendingDelete, setRulePendingDelete] =
    useState<ShadowMCPAccessRule | null>(null);

  useEffect(() => {
    setRulePendingDelete(null);
  }, [projectId]);

  const ruleDisposition =
    ruleDispositionFilter === "all" ? undefined : ruleDispositionFilter;
  const hasActiveRuleFilter = ruleDispositionFilter !== "all";

  const requestsQuery = useInfiniteQuery({
    queryKey: [...APPROVAL_REQUESTS_QUERY_KEY, projectId, "requested"],
    queryFn: ({ pageParam }) =>
      client.access.listShadowMCPApprovalRequests({
        limit: APPROVAL_REQUESTS_PAGE_SIZE,
        projectId,
        status: "requested",
        cursor: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    enabled: canAdmin && projectId.length > 0,
  });
  const rulesQuery = useInfiniteQuery({
    queryKey: [...APPROVAL_REQUEST_RULES_QUERY_KEY, projectId, ruleDisposition],
    queryFn: ({ pageParam }) =>
      client.access.listShadowMCPAccessRules({
        limit: APPROVAL_REQUESTS_PAGE_SIZE,
        accessScope: "project",
        projectId,
        disposition: ruleDisposition,
        cursor: pageParam,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    enabled: projectId.length > 0,
  });
  const policiesQuery = useRiskListPolicies(undefined, undefined, {
    enabled: canAdmin && projectId.length > 0,
  });

  const requests = useMemo(
    () => requestsQuery.data?.pages.flatMap((page) => page.requests) ?? [],
    [requestsQuery.data?.pages],
  );
  const rules = useMemo(
    () => rulesQuery.data?.pages.flatMap((page) => page.rules) ?? [],
    [rulesQuery.data?.pages],
  );
  const canCreateAccessRules = useMemo(
    () =>
      // Policies are only fetched for admins; without policy data we can't
      // know rules are inactive, so default to treating them as active.
      !canAdmin || policiesQuery.error
        ? true
        : (policiesQuery.data?.policies.some(policyEnablesAccessRules) ??
          false),
    [canAdmin, policiesQuery.data?.policies, policiesQuery.error],
  );
  const accessRuleCreateAvailability = getAccessRuleCreateAvailability({
    canCreateAccessRules,
    isLoadingPolicies: policiesQuery.isLoading,
  });
  const inactiveAccessRules =
    accessRuleCreateAvailability.status === "missing_blocking_policy";
  const addRuleDisabledReason =
    accessRuleCreateAvailability.status === "available"
      ? undefined
      : accessRuleCreateAvailability.reason;
  const accessRulesEmptyDescription = getAccessRulesEmptyDescription(
    accessRuleCreateAvailability,
  );
  const showManagePoliciesEmptyAction =
    accessRuleCreateAvailability.status === "missing_blocking_policy";
  const requestsLoading = requestsQuery.isLoading;
  const requestsError = requestsQuery.error;
  const rulesLoading = rulesQuery.isLoading;
  const rulesError = rulesQuery.error;

  const renderPaginationFooter = ({
    count,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    noun,
    onLoadMore,
    showEndMessage,
  }: {
    count: number;
    hasNextPage: boolean;
    isFetching: boolean;
    isFetchingNextPage: boolean;
    noun: string;
    onLoadMore: () => void;
    showEndMessage: boolean;
  }) => (
    <div className="bg-muted/20 flex items-center justify-between border-t px-4 py-3">
      <Type muted small>
        {count.toLocaleString()} {noun}
        {count === 1 ? "" : "s"}
      </Type>

      {hasNextPage ? (
        <Button
          variant="secondary"
          size="sm"
          onClick={onLoadMore}
          disabled={isFetchingNextPage}
        >
          <Button.Text>
            {isFetchingNextPage ? "Loading..." : "Load more"}
          </Button.Text>
        </Button>
      ) : isFetching || showEndMessage ? (
        <Type muted small>
          {isFetching ? "Refreshing..." : `End of ${noun} list`}
        </Type>
      ) : null}
    </div>
  );

  const approveRequest = useApproveShadowMCPApprovalRequestMutation();
  const denyRequest = useDenyShadowMCPApprovalRequestMutation();
  const createRule = useCreateShadowMCPAccessRuleMutation();
  const updateRule = useUpdateShadowMCPAccessRuleMutation();
  const deleteRule = useDeleteShadowMCPAccessRuleMutation();
  const isReviewSubmitting = approveRequest.isPending || denyRequest.isPending;
  const isRuleSubmitting =
    createRule.isPending || updateRule.isPending || deleteRule.isPending;

  const refreshApprovalRequestsData = async () => {
    await Promise.all([
      invalidateAllShadowMCPApprovalRequests(queryClient),
      invalidateAllShadowMCPAccessRules(queryClient),
      queryClient.invalidateQueries({
        queryKey: APPROVAL_REQUESTS_QUERY_KEY,
      }),
      queryClient.invalidateQueries({
        queryKey: APPROVAL_REQUEST_RULES_QUERY_KEY,
      }),
    ]);
  };

  const closeDeleteRuleDialog = () => {
    if (deleteRule.isPending) return;
    setRulePendingDelete(null);
  };

  const confirmDeleteRule = async () => {
    if (!rulePendingDelete || deleteRule.isPending) return;

    try {
      await deleteRule.mutateAsync({ request: { id: rulePendingDelete.id } });
      await refreshApprovalRequestsData();
      toast.success("Access Rule deleted");
      setRulePendingDelete(null);
    } catch {
      toast.error("Access Rule delete failed");
    }
  };

  const requestColumns: Column<ShadowMCPApprovalRequest>[] = [
    {
      key: "server",
      header: "Server",
      width: "1.5fr",
      render: (request) => (
        <ServerCell
          name={getRequestDisplayName(request)}
          detail={getRequestServerDetail(request)}
        />
      ),
    },
    {
      key: "resourceType",
      header: "Type",
      width: "0.75fr",
      render: (request) => (
        <Badge variant="neutral">
          <Badge.Text>{getResourceTypeLabel(request.resourceType)}</Badge.Text>
        </Badge>
      ),
    },
    {
      key: "requester",
      header: "Requester",
      width: "1.25fr",
      render: (request) => (
        <ServerCell
          name={getRequesterLabel(request)}
          detail={getRequesterDetail(request)}
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
      key: "blocked",
      header: "Blocked",
      width: "0.5fr",
      render: (request) => (
        <Type variant="small">
          {request.blockedCount}{" "}
          <span className="text-muted-foreground">times</span>
        </Type>
      ),
    },
    {
      key: "lastBlocked",
      header: "Last blocked",
      width: "0.75fr",
      render: (request) => (
        <Type variant="small">{formatShortDate(request.lastBlockedAt)}</Type>
      ),
    },
  ];
  const pendingRequestColumns: Column<ShadowMCPApprovalRequest>[] = [
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

  const ruleColumns: Column<ShadowMCPAccessRule>[] = [
    {
      key: "server",
      header: "Server",
      width: "1.5fr",
      render: (rule) => (
        <AccessRuleTableCell inactive={inactiveAccessRules}>
          <ServerCell
            name={getRuleDisplayName(rule)}
            detail={getRuleServerDetail(rule)}
          />
        </AccessRuleTableCell>
      ),
    },
    {
      key: "resourceType",
      header: "Type",
      width: "0.75fr",
      render: (rule) => (
        <AccessRuleTableCell inactive={inactiveAccessRules}>
          <Badge variant="neutral">
            <Badge.Text>{getResourceTypeLabel(rule.resourceType)}</Badge.Text>
          </Badge>
        </AccessRuleTableCell>
      ),
    },
    {
      key: "match",
      header: "Match",
      width: "1.25fr",
      render: (rule) => (
        <AccessRuleTableCell inactive={inactiveAccessRules}>
          <div className="min-w-0 space-y-1">
            <Type variant="small" className="font-medium">
              {getMatchBreadthLabel(rule.matchBreadth)}
            </Type>
            <Type
              variant="small"
              className="text-muted-foreground truncate text-xs"
            >
              {rule.matchValue}
            </Type>
          </div>
        </AccessRuleTableCell>
      ),
    },
    {
      key: "disposition",
      header: "Status",
      width: "0.5fr",
      render: (rule) => (
        <AccessRuleTableCell inactive={inactiveAccessRules}>
          <RuleDispositionBadge disposition={rule.disposition} />
        </AccessRuleTableCell>
      ),
    },
    {
      key: "scope",
      header: "Scope",
      width: "0.5fr",
      render: (rule) => (
        <AccessRuleTableCell inactive={inactiveAccessRules}>
          <Type variant="small">
            {rule.accessScope === "project"
              ? projectName(organization.projects, rule.projectId)
              : getAccessScopeLabel(rule.accessScope)}
          </Type>
        </AccessRuleTableCell>
      ),
    },
    {
      key: "updated",
      header: "Updated",
      width: "0.75fr",
      render: (rule) => (
        <AccessRuleTableCell inactive={inactiveAccessRules}>
          <Type variant="small">{formatShortDate(rule.updatedAt)}</Type>
        </AccessRuleTableCell>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.5fr",
      render: (rule) => (
        <RuleActionsMenu
          onEdit={() => {
            setEditingRule(rule);
            setIsRuleSheetOpen(true);
          }}
          onDelete={() => setRulePendingDelete(rule)}
        />
      ),
    },
  ];

  const openCreateRuleSheet = () => {
    setEditingRule(null);
    setIsRuleSheetOpen(true);
  };

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
        onApprove={async (input) => {
          if (!reviewRequest) return;

          await approveRequest.mutateAsync({
            request: {
              approveShadowMCPApprovalRequestForm: {
                id: reviewRequest.id,
                displayName: input.displayName,
                accessScope: "project",
                projectIds: input.projectIds,
                matchBreadth: input.matchBreadth,
                matchValue: input.matchValue,
                observedFullUrl: reviewRequest.observedFullUrl,
                observedServerIdentity: reviewRequest.observedServerIdentity,
                observedUrlHost: reviewRequest.observedUrlHost,
                reason: input.reason,
              },
            },
          });
          await refreshApprovalRequestsData();
          toast.success("Request approved");
          setReviewRequest(null);
        }}
        onDeny={async (input) => {
          if (!reviewRequest) return;

          await denyRequest.mutateAsync({
            request: {
              denyShadowMCPApprovalRequestForm: {
                id: reviewRequest.id,
                createDenyRule: input.createDenyRule,
                projectIds: input.projectIds,
                displayName: input.displayName,
                matchBreadth: input.matchBreadth,
                matchValue: input.matchValue,
                observedFullUrl: reviewRequest.observedFullUrl,
                observedServerIdentity: reviewRequest.observedServerIdentity,
                observedUrlHost: reviewRequest.observedUrlHost,
                reason: input.reason,
              },
            },
          });
          await refreshApprovalRequestsData();
          toast.success("Request denied");
          setReviewRequest(null);
        }}
      />

      <AccessRuleSheet
        rule={editingRule}
        projectId={projectId}
        open={isRuleSheetOpen}
        isSubmitting={isRuleSubmitting}
        onOpenChange={(open) => {
          setIsRuleSheetOpen(open);
          if (!open) setEditingRule(null);
        }}
        onSubmit={async (input) => {
          if (editingRule) {
            await updateRule.mutateAsync({
              request: {
                updateShadowMCPAccessRuleForm: {
                  id: editingRule.id,
                  displayName: input.displayName,
                  disposition: input.disposition,
                  accessScope: "project",
                  projectId: input.projectId,
                  matchBreadth: input.matchBreadth,
                  matchValue: input.matchValue,
                  observedFullUrl:
                    input.matchBreadth === "full_url"
                      ? input.matchValue
                      : undefined,
                  observedUrlHost:
                    input.matchBreadth === "url_host"
                      ? input.matchValue
                      : undefined,
                  reason: input.reason,
                },
              },
            });
            toast.success("Access Rule updated");
          } else {
            await createRule.mutateAsync({
              request: {
                createShadowMCPAccessRuleForm: {
                  displayName: input.displayName,
                  disposition: input.disposition,
                  accessScope: "project",
                  projectIds: input.projectIds,
                  matchBreadth: input.matchBreadth,
                  matchValue: input.matchValue,
                  observedFullUrl:
                    input.matchBreadth === "full_url"
                      ? input.matchValue
                      : undefined,
                  observedUrlHost:
                    input.matchBreadth === "url_host"
                      ? input.matchValue
                      : undefined,
                  reason: input.reason,
                },
              },
            });
            toast.success("Access Rule created");
          }

          await refreshApprovalRequestsData();
          setIsRuleSheetOpen(false);
          setEditingRule(null);
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
              {renderPaginationFooter({
                count: requests.length,
                hasNextPage: requestsQuery.hasNextPage,
                isFetching: requestsQuery.isFetching,
                isFetchingNextPage: requestsQuery.isFetchingNextPage,
                noun: "request",
                onLoadMore: () => {
                  void requestsQuery.fetchNextPage();
                },
                showEndMessage: (requestsQuery.data?.pages.length ?? 0) > 1,
              })}
            </div>
          )}
        </section>
      )}

      <section className="space-y-4">
        <div className="flex items-start justify-between gap-4">
          <div>
            <Heading variant="h5">Access Rules</Heading>
            <Type muted small className="mt-1">
              Manage resources that are explicitly allowed or denied.
            </Type>
          </div>

          {(rules.length > 0 || hasActiveRuleFilter) && (
            <div className="flex shrink-0 flex-wrap justify-end gap-2">
              <Select
                value={ruleDispositionFilter}
                onValueChange={(value) =>
                  setRuleDispositionFilter(value as RuleDispositionFilter)
                }
              >
                <SelectTrigger className="w-[150px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All rules</SelectItem>
                  <SelectItem value="allowed">Allowed</SelectItem>
                  <SelectItem value="denied">Denied</SelectItem>
                </SelectContent>
              </Select>
              <RequireScope scope="org:admin" level="component">
                <AddRuleButton
                  disabled={!!addRuleDisabledReason}
                  disabledReason={addRuleDisabledReason}
                  onClick={openCreateRuleSheet}
                />
              </RequireScope>
            </div>
          )}
        </div>

        {rulesLoading ? (
          <SkeletonTable />
        ) : rulesError ? (
          <TableEmptyState
            title="Access Rules could not be loaded"
            description="Refresh the page or try again later."
          />
        ) : rules.length === 0 && !hasActiveRuleFilter ? (
          <ApprovalSectionEmptyState
            icon={ShieldCheck}
            title="No access rules"
            description={accessRulesEmptyDescription}
            action={
              showManagePoliciesEmptyAction ? (
                <ManagePoliciesButton />
              ) : (
                <RequireScope scope="org:admin" level="component">
                  <AddRuleButton
                    disabled={!!addRuleDisabledReason}
                    disabledReason={addRuleDisabledReason}
                    onClick={openCreateRuleSheet}
                  />
                </RequireScope>
              )
            }
          />
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table
              columns={ruleColumns}
              data={rules}
              rowKey={(row) => row.id}
              className="[&_thead]:bg-background max-h-128 overflow-y-auto rounded-none border-0 [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
              noResultsMessage={
                <div className="bg-muted/20 flex justify-center p-6 text-center">
                  <Type variant="subheading">No matching rules</Type>
                </div>
              }
            />
            {renderPaginationFooter({
              count: rules.length,
              hasNextPage: rulesQuery.hasNextPage,
              isFetching: rulesQuery.isFetching,
              isFetchingNextPage: rulesQuery.isFetchingNextPage,
              noun: "rule",
              onLoadMore: () => {
                void rulesQuery.fetchNextPage();
              },
              showEndMessage: (rulesQuery.data?.pages.length ?? 0) > 1,
            })}
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
            <Dialog.Title>Delete access rule</Dialog.Title>
          </Dialog.Header>
          <Type variant="small">
            This action cannot be undone. Are you sure you want to delete{" "}
            <code className="bg-muted rounded px-1 py-0.5 font-mono font-bold">
              {rulePendingDelete?.displayName ?? "this access rule"}
            </code>
            ?
          </Type>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={closeDeleteRuleDialog}
              disabled={deleteRule.isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              onClick={() => void confirmDeleteRule()}
              disabled={deleteRule.isPending}
            >
              {deleteRule.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Deleting</Button.Text>
                </>
              ) : (
                <Button.Text>Delete</Button.Text>
              )}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}
