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
import { cn } from "@/lib/utils";
import type { ShadowMCPAccessRule } from "@gram/client/models/components/shadowmcpaccessrule.js";
import type { ShadowMCPApprovalRequest } from "@gram/client/models/components/shadowmcpapprovalrequest.js";
import {
  invalidateAllRoles,
  useRoles,
} from "@gram/client/react-query/roles.js";
import { useApproveShadowMCPApprovalRequestMutation } from "@gram/client/react-query/approveShadowMCPApprovalRequest.js";
import { useCreateShadowMCPAccessRuleMutation } from "@gram/client/react-query/createShadowMCPAccessRule.js";
import { useDeleteShadowMCPAccessRuleMutation } from "@gram/client/react-query/deleteShadowMCPAccessRule.js";
import { useDenyShadowMCPApprovalRequestMutation } from "@gram/client/react-query/denyShadowMCPApprovalRequest.js";
import {
  invalidateAllShadowMCPAccessRules,
  useShadowMCPAccessRules,
} from "@gram/client/react-query/shadowMCPAccessRules.js";
import {
  invalidateAllShadowMCPApprovalRequests,
  useShadowMCPApprovalRequests,
} from "@gram/client/react-query/shadowMCPApprovalRequests.js";
import { useUpdateShadowMCPAccessRuleMutation } from "@gram/client/react-query/updateShadowMCPAccessRule.js";
import {
  Badge,
  Button,
  Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Table,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Ellipsis, Plus } from "lucide-react";
import type React from "react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  formatShortDate,
  getDefaultMatchBreadth,
  getDispositionLabel,
  getMatchBreadthLabel,
  getMatchValue,
  getRequesterDetail,
  getRequesterLabel,
  getRequestDisplayName,
  getRequestStatusLabel,
  getRuleDisplayName,
  roleNamesForIds,
  roleOptionsFromRoles,
  type ShadowMCPDisposition,
  type ShadowMCPMatchBreadth,
  type ShadowMCPRoleOption,
} from "./shadow-mcp-utils";

type RequestStatusFilter = "requested" | "approved" | "denied" | "all";
type RuleDispositionFilter = "allowed" | "denied" | "all";
type ReviewAction = "approve" | "deny";

const MATCH_BREADTH_OPTIONS: {
  value: ShadowMCPMatchBreadth;
  label: string;
}[] = [
  { value: "full_url", label: "Full URL" },
  { value: "url_host", label: "URL host" },
  { value: "server_identity", label: "Server identity" },
];

function SectionHeader({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: React.ReactNode;
}) {
  return (
    <div className="mb-3 flex items-start justify-between gap-4">
      <div>
        <Heading variant="h4">{title}</Heading>
        <Type muted small className="mt-1">
          {description}
        </Type>
      </div>
      {action}
    </div>
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

function ServerCell({
  name,
  detail,
}: {
  name: string;
  detail?: string | null;
}) {
  return (
    <div className="min-w-0 space-y-1">
      <Type variant="body" className="truncate font-medium">
        {name}
      </Type>
      {detail && (
        <Type variant="body" className="text-muted-foreground truncate text-xs">
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
        : "outline";

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

function RoleSummary({
  roleIds,
  roleNames,
}: {
  roleIds: string[];
  roleNames: string[];
}) {
  if (roleIds.length === 0) {
    return (
      <Type variant="body" className="text-muted-foreground text-sm">
        No role grants
      </Type>
    );
  }

  const visible = roleNames.slice(0, 2);
  const hiddenCount = roleNames.length - visible.length;

  return (
    <div className="flex flex-wrap gap-1">
      {visible.map((roleName) => (
        <Badge key={roleName} variant="neutral">
          <Badge.Text>{roleName}</Badge.Text>
        </Badge>
      ))}
      {hiddenCount > 0 && (
        <Badge variant="neutral">
          <Badge.Text>+{hiddenCount}</Badge.Text>
        </Badge>
      )}
    </div>
  );
}

function RolePicker({
  roles,
  selectedRoleIds,
  onChange,
}: {
  roles: ShadowMCPRoleOption[];
  selectedRoleIds: string[];
  onChange: (roleIds: string[]) => void;
}) {
  const editableRoles = roles.filter((role) => !role.isSystem);

  if (editableRoles.length === 0) {
    return (
      <div className="border-border bg-muted/30 rounded-md border px-3 py-3">
        <Type muted small>
          Create a custom role before assigning Shadow MCP access.
        </Type>
      </div>
    );
  }

  const toggleRole = (roleId: string) => {
    if (selectedRoleIds.includes(roleId)) {
      onChange(selectedRoleIds.filter((id) => id !== roleId));
    } else {
      onChange([...selectedRoleIds, roleId]);
    }
  };

  return (
    <div className="border-border divide-border divide-y rounded-md border">
      {editableRoles.map((role) => (
        <label
          key={role.id}
          className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 px-3 py-2.5"
        >
          <Checkbox
            checked={selectedRoleIds.includes(role.id)}
            onCheckedChange={() => toggleRole(role.id)}
            className="mt-0.5"
          />
          <div className="min-w-0 flex-1">
            <Type variant="body" className="text-sm font-medium">
              {role.name}
            </Type>
            {role.description && (
              <Type
                variant="body"
                className="text-muted-foreground mt-0.5 text-xs"
              >
                {role.description}
              </Type>
            )}
          </div>
        </label>
      ))}
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
    <label className="space-y-2">
      <Type variant="body" className="text-sm font-medium">
        {label}
      </Type>
      {children}
    </label>
  );
}

function ReviewRequestSheet({
  request,
  roles,
  open,
  isSubmitting,
  onOpenChange,
  onApprove,
  onDeny,
}: {
  request: ShadowMCPApprovalRequest | null;
  roles: ShadowMCPRoleOption[];
  open: boolean;
  isSubmitting: boolean;
  onOpenChange: (open: boolean) => void;
  onApprove: (input: {
    displayName: string;
    matchBreadth: ShadowMCPMatchBreadth;
    matchValue: string;
    roleIds: string[];
    reason?: string;
  }) => Promise<void>;
  onDeny: (input: {
    createDenyRule: boolean;
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
  const [roleIds, setRoleIds] = useState<string[]>([]);
  const [reason, setReason] = useState("");
  const [createDenyRule, setCreateDenyRule] = useState(true);

  useEffect(() => {
    if (!request || !open) return;

    const nextMatchBreadth = getDefaultMatchBreadth(request);
    setAction("approve");
    setDisplayName(getRequestDisplayName(request));
    setMatchBreadth(nextMatchBreadth);
    setMatchValue(getMatchValue(request, nextMatchBreadth));
    setRoleIds([]);
    setReason("");
    setCreateDenyRule(true);
  }, [request, open]);

  if (!request) return null;

  const canSubmit =
    action === "approve"
      ? displayName.trim().length > 0 &&
        matchValue.trim().length > 0 &&
        roleIds.length > 0
      : !createDenyRule ||
        (displayName.trim().length > 0 && matchValue.trim().length > 0);

  const submit = async () => {
    const trimmedReason = reason.trim() || undefined;

    try {
      if (action === "approve") {
        await onApprove({
          displayName: displayName.trim(),
          matchBreadth,
          matchValue: matchValue.trim(),
          roleIds,
          reason: trimmedReason,
        });
      } else {
        await onDeny({
          createDenyRule,
          displayName: createDenyRule ? displayName.trim() : undefined,
          matchBreadth: createDenyRule ? matchBreadth : undefined,
          matchValue: createDenyRule ? matchValue.trim() : undefined,
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
            Decide how this Shadow MCP server should be handled.
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-4">
          <div className="border-border rounded-md border px-3 py-3">
            <ServerCell
              name={getRequestDisplayName(request)}
              detail={request.observedFullUrl ?? request.observedUrlHost}
            />
            <div className="mt-3 grid grid-cols-2 gap-3">
              <div>
                <Type muted small>
                  Requester
                </Type>
                <Type variant="body" className="mt-1 text-sm">
                  {getRequesterLabel(request)}
                </Type>
              </div>
              <div>
                <Type muted small>
                  Last blocked
                </Type>
                <Type variant="body" className="mt-1 text-sm">
                  {formatShortDate(request.lastBlockedAt)}
                </Type>
              </div>
            </div>
          </div>

          <RadioGroup
            value={action}
            onValueChange={(value) => setAction(value as ReviewAction)}
            className="grid grid-cols-2 gap-3"
          >
            <label className="border-border has-[[data-state=checked]]:border-primary flex cursor-pointer gap-3 rounded-md border p-3">
              <RadioGroupItem value="approve" className="mt-1" />
              <span>
                <Type variant="body" className="font-medium">
                  Approve
                </Type>
                <Type muted small>
                  Create an allow rule.
                </Type>
              </span>
            </label>
            <label className="border-border has-[[data-state=checked]]:border-primary flex cursor-pointer gap-3 rounded-md border p-3">
              <RadioGroupItem value="deny" className="mt-1" />
              <span>
                <Type variant="body" className="font-medium">
                  Deny
                </Type>
                <Type muted small>
                  Reject the request.
                </Type>
              </span>
            </label>
          </RadioGroup>

          {(action === "approve" || createDenyRule) && (
            <>
              <Field label="Display name">
                <Input
                  value={displayName}
                  onChange={(event) => setDisplayName(event.target.value)}
                />
              </Field>

              <div className="grid grid-cols-[160px_1fr] gap-3">
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
              </div>
            </>
          )}

          {action === "approve" ? (
            <Field label="Roles">
              <RolePicker
                roles={roles}
                selectedRoleIds={roleIds}
                onChange={setRoleIds}
              />
            </Field>
          ) : (
            <label className="flex items-start gap-3">
              <Checkbox
                checked={createDenyRule}
                onCheckedChange={(checked) => setCreateDenyRule(!!checked)}
                className="mt-0.5"
              />
              <span>
                <Type variant="body" className="text-sm font-medium">
                  Create deny rule
                </Type>
                <Type muted small>
                  Future matching calls will be blocked explicitly.
                </Type>
              </span>
            </label>
          )}

          <Field label="Reason">
            <Textarea
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder="Optional"
            />
          </Field>
        </div>

        <SheetFooter>
          <Button
            onClick={submit}
            disabled={!canSubmit || isSubmitting}
            className="w-full"
          >
            <Button.Text>
              {action === "approve" ? "Approve request" : "Deny request"}
            </Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function AccessRuleSheet({
  rule,
  roles,
  open,
  isSubmitting,
  onOpenChange,
  onSubmit,
}: {
  rule: ShadowMCPAccessRule | null;
  roles: ShadowMCPRoleOption[];
  open: boolean;
  isSubmitting: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    displayName: string;
    disposition: ShadowMCPDisposition;
    matchBreadth: ShadowMCPMatchBreadth;
    matchValue: string;
    roleIds?: string[];
    reason?: string;
  }) => Promise<void>;
}) {
  const [disposition, setDisposition] =
    useState<ShadowMCPDisposition>("allowed");
  const [displayName, setDisplayName] = useState("");
  const [matchBreadth, setMatchBreadth] =
    useState<ShadowMCPMatchBreadth>("full_url");
  const [matchValue, setMatchValue] = useState("");
  const [roleIds, setRoleIds] = useState<string[]>([]);
  const [reason, setReason] = useState("");

  useEffect(() => {
    if (!open) return;

    if (rule) {
      setDisposition(rule.disposition);
      setDisplayName(rule.displayName);
      setMatchBreadth(rule.matchBreadth);
      setMatchValue(rule.matchValue);
      setRoleIds(rule.roleIds);
      setReason(rule.reason ?? "");
      return;
    }

    setDisposition("allowed");
    setDisplayName("");
    setMatchBreadth("full_url");
    setMatchValue("");
    setRoleIds([]);
    setReason("");
  }, [rule, open]);

  const canSubmit =
    displayName.trim().length > 0 &&
    matchValue.trim().length > 0 &&
    (disposition === "denied" || roleIds.length > 0);

  const submit = async () => {
    try {
      await onSubmit({
        displayName: displayName.trim(),
        disposition,
        matchBreadth,
        matchValue: matchValue.trim(),
        roleIds: disposition === "allowed" ? roleIds : undefined,
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
            Configure a Shadow MCP allow or deny decision.
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-4">
          <RadioGroup
            value={disposition}
            onValueChange={(value) =>
              setDisposition(value as ShadowMCPDisposition)
            }
            className="grid grid-cols-2 gap-3"
          >
            <label className="border-border has-[[data-state=checked]]:border-primary flex cursor-pointer gap-3 rounded-md border p-3">
              <RadioGroupItem value="allowed" className="mt-1" />
              <span>
                <Type variant="body" className="font-medium">
                  Allow
                </Type>
                <Type muted small>
                  Grant selected roles access.
                </Type>
              </span>
            </label>
            <label className="border-border has-[[data-state=checked]]:border-primary flex cursor-pointer gap-3 rounded-md border p-3">
              <RadioGroupItem value="denied" className="mt-1" />
              <span>
                <Type variant="body" className="font-medium">
                  Deny
                </Type>
                <Type muted small>
                  Block matching calls.
                </Type>
              </span>
            </label>
          </RadioGroup>

          <Field label="Display name">
            <Input
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              placeholder="Datadog"
            />
          </Field>

          <div className="grid grid-cols-[160px_1fr] gap-3">
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
          </div>

          {disposition === "allowed" && (
            <Field label="Roles">
              <RolePicker
                roles={roles}
                selectedRoleIds={roleIds}
                onChange={setRoleIds}
              />
            </Field>
          )}

          <Field label="Reason">
            <Textarea
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              placeholder="Optional"
            />
          </Field>
        </div>

        <SheetFooter>
          <Button
            onClick={submit}
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
          <DropdownMenuItem onSelect={() => setTimeout(onEdit, 0)}>
            Edit
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => setTimeout(onDelete, 0)}>
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </RequireScope>
  );
}

export function ShadowMCPAccessContent() {
  const queryClient = useQueryClient();
  const [requestStatusFilter, setRequestStatusFilter] =
    useState<RequestStatusFilter>("requested");
  const [ruleDispositionFilter, setRuleDispositionFilter] =
    useState<RuleDispositionFilter>("all");
  const [reviewRequest, setReviewRequest] =
    useState<ShadowMCPApprovalRequest | null>(null);
  const [isRuleSheetOpen, setIsRuleSheetOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<ShadowMCPAccessRule | null>(
    null,
  );

  const { data: rolesData } = useRoles();
  const roles = useMemo(
    () => roleOptionsFromRoles(rolesData?.roles ?? []),
    [rolesData],
  );
  const {
    data: requestsData,
    isLoading: requestsLoading,
    error: requestsError,
  } = useShadowMCPApprovalRequests({
    limit: 100,
    status: requestStatusFilter === "all" ? undefined : requestStatusFilter,
  });
  const {
    data: rulesData,
    isLoading: rulesLoading,
    error: rulesError,
  } = useShadowMCPAccessRules({
    limit: 100,
    disposition:
      ruleDispositionFilter === "all" ? undefined : ruleDispositionFilter,
  });

  const requests = requestsData?.requests ?? [];
  const rules = rulesData?.rules ?? [];
  const approveRequest = useApproveShadowMCPApprovalRequestMutation();
  const denyRequest = useDenyShadowMCPApprovalRequestMutation();
  const createRule = useCreateShadowMCPAccessRuleMutation();
  const updateRule = useUpdateShadowMCPAccessRuleMutation();
  const deleteRule = useDeleteShadowMCPAccessRuleMutation();
  const isReviewSubmitting = approveRequest.isPending || denyRequest.isPending;
  const isRuleSubmitting =
    createRule.isPending || updateRule.isPending || deleteRule.isPending;

  const refreshShadowMCPData = async () => {
    await Promise.all([
      invalidateAllShadowMCPApprovalRequests(queryClient),
      invalidateAllShadowMCPAccessRules(queryClient),
      invalidateAllRoles(queryClient),
    ]);
  };

  const requestColumns: Column<ShadowMCPApprovalRequest>[] = [
    {
      key: "server",
      header: "Server",
      width: "minmax(240px, 1.4fr)",
      render: (request) => (
        <ServerCell
          name={getRequestDisplayName(request)}
          detail={request.observedFullUrl ?? request.observedUrlHost}
        />
      ),
    },
    {
      key: "requester",
      header: "Requester",
      width: "minmax(180px, 1fr)",
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
      width: "120px",
      render: (request) => <RequestStatusBadge status={request.status} />,
    },
    {
      key: "blocked",
      header: "Blocked",
      width: "140px",
      render: (request) => (
        <Type variant="body">
          {request.blockedCount}{" "}
          <span className="text-muted-foreground">times</span>
        </Type>
      ),
    },
    {
      key: "lastBlocked",
      header: "Last blocked",
      width: "160px",
      render: (request) => (
        <Type variant="body">{formatShortDate(request.lastBlockedAt)}</Type>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "96px",
      render: (request) => (
        <RequireScope scope="org:admin" level="component">
          <Button
            size="sm"
            disabled={request.status !== "requested"}
            onClick={() => setReviewRequest(request)}
          >
            <Button.Text>Review</Button.Text>
          </Button>
        </RequireScope>
      ),
    },
  ];

  const ruleColumns: Column<ShadowMCPAccessRule>[] = [
    {
      key: "disposition",
      header: "",
      width: "110px",
      render: (rule) => <RuleDispositionBadge disposition={rule.disposition} />,
    },
    {
      key: "server",
      header: "Server",
      width: "minmax(240px, 1.4fr)",
      render: (rule) => (
        <ServerCell
          name={getRuleDisplayName(rule)}
          detail={rule.observedFullUrl ?? rule.matchValue}
        />
      ),
    },
    {
      key: "match",
      header: "Match",
      width: "minmax(180px, 1fr)",
      render: (rule) => (
        <div className="min-w-0 space-y-1">
          <Type variant="body" className="font-medium">
            {getMatchBreadthLabel(rule.matchBreadth)}
          </Type>
          <Type
            variant="body"
            className="text-muted-foreground truncate text-xs"
          >
            {rule.matchValue}
          </Type>
        </div>
      ),
    },
    {
      key: "roles",
      header: "Roles",
      width: "minmax(160px, 1fr)",
      render: (rule) => (
        <RoleSummary
          roleIds={rule.roleIds}
          roleNames={roleNamesForIds(rule.roleIds, roles)}
        />
      ),
    },
    {
      key: "updated",
      header: "Updated",
      width: "140px",
      render: (rule) => (
        <Type variant="body">{formatShortDate(rule.updatedAt)}</Type>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (rule) => (
        <RuleActionsMenu
          onEdit={() => {
            setEditingRule(rule);
            setIsRuleSheetOpen(true);
          }}
          onDelete={async () => {
            if (!window.confirm(`Delete Access Rule "${rule.displayName}"?`)) {
              return;
            }

            try {
              await deleteRule.mutateAsync({ request: { id: rule.id } });
              await refreshShadowMCPData();
              toast.success("Access Rule deleted");
            } catch {
              toast.error("Access Rule delete failed");
            }
          }}
        />
      ),
    },
  ];

  return (
    <div className="space-y-8">
      <ReviewRequestSheet
        request={reviewRequest}
        roles={roles}
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
                matchBreadth: input.matchBreadth,
                matchValue: input.matchValue,
                observedFullUrl: reviewRequest.observedFullUrl,
                observedServerIdentity: reviewRequest.observedServerIdentity,
                observedUrlHost: reviewRequest.observedUrlHost,
                reason: input.reason,
                roleIds: input.roleIds,
              },
            },
          });
          await refreshShadowMCPData();
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
          await refreshShadowMCPData();
          toast.success("Request denied");
          setReviewRequest(null);
        }}
      />

      <AccessRuleSheet
        rule={editingRule}
        roles={roles}
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
                  matchBreadth: input.matchBreadth,
                  matchValue: input.matchValue,
                  observedFullUrl:
                    input.matchBreadth === "full_url"
                      ? input.matchValue
                      : editingRule.observedFullUrl,
                  observedServerIdentity:
                    input.matchBreadth === "server_identity"
                      ? input.matchValue
                      : editingRule.observedServerIdentity,
                  observedUrlHost:
                    input.matchBreadth === "url_host"
                      ? input.matchValue
                      : editingRule.observedUrlHost,
                  reason: input.reason,
                  roleIds: input.roleIds,
                },
              },
            });
            toast.success("Access Rule updated");
          } else {
            await createRule.mutateAsync({
              request: {
                shadowMCPAccessRuleForm: {
                  displayName: input.displayName,
                  disposition: input.disposition,
                  matchBreadth: input.matchBreadth,
                  matchValue: input.matchValue,
                  observedFullUrl:
                    input.matchBreadth === "full_url"
                      ? input.matchValue
                      : undefined,
                  observedServerIdentity:
                    input.matchBreadth === "server_identity"
                      ? input.matchValue
                      : undefined,
                  observedUrlHost:
                    input.matchBreadth === "url_host"
                      ? input.matchValue
                      : undefined,
                  reason: input.reason,
                  roleIds: input.roleIds,
                },
              },
            });
            toast.success("Access Rule created");
          }

          await refreshShadowMCPData();
          setIsRuleSheetOpen(false);
          setEditingRule(null);
        }}
      />

      <section>
        <div className="mb-4 flex items-start justify-between gap-4">
          <SectionHeader
            title="Requests"
            description="Review Shadow MCP servers users have requested after a policy block."
          />
          <Select
            value={requestStatusFilter}
            onValueChange={(value) =>
              setRequestStatusFilter(value as RequestStatusFilter)
            }
          >
            <SelectTrigger className="w-36">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="requested">Requested</SelectItem>
              <SelectItem value="approved">Approved</SelectItem>
              <SelectItem value="denied">Denied</SelectItem>
              <SelectItem value="all">All</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {requestsLoading ? (
          <SkeletonTable />
        ) : requestsError ? (
          <TableEmptyState
            title="Requests could not be loaded"
            description="Refresh the page or try again later."
          />
        ) : requests.length === 0 ? (
          <TableEmptyState
            title="No requests"
            description="Blocked Shadow MCP servers will appear here after a user requests access."
          />
        ) : (
          <Table
            columns={requestColumns}
            data={requests}
            rowKey={(row) => row.id}
            className="max-h-[520px] [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
          />
        )}
      </section>

      <section className="border-border border-t pt-8">
        <SectionHeader
          title="Access Rules"
          description="Manage the Shadow MCP servers that are explicitly allowed or denied."
          action={
            <RequireScope scope="org:admin" level="component">
              <Button
                onClick={() => {
                  setEditingRule(null);
                  setIsRuleSheetOpen(true);
                }}
              >
                <Button.LeftIcon>
                  <Plus className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Add Rule</Button.Text>
              </Button>
            </RequireScope>
          }
        />

        <div className="mb-4">
          <Select
            value={ruleDispositionFilter}
            onValueChange={(value) =>
              setRuleDispositionFilter(value as RuleDispositionFilter)
            }
          >
            <SelectTrigger className="w-32">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All rules</SelectItem>
              <SelectItem value="allowed">Allowed</SelectItem>
              <SelectItem value="denied">Denied</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {rulesLoading ? (
          <SkeletonTable />
        ) : rulesError ? (
          <TableEmptyState
            title="Access Rules could not be loaded"
            description="Refresh the page or try again later."
          />
        ) : rules.length === 0 ? (
          <TableEmptyState
            title="No Access Rules"
            description="Create a rule manually or approve a request to make a Shadow MCP decision available for enforcement."
          />
        ) : (
          <Table
            columns={ruleColumns}
            data={rules}
            rowKey={(row) => row.id}
            className="max-h-[520px] [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
          />
        )}
      </section>
    </div>
  );
}
