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
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Type } from "@/components/ui/type";
import type { Role } from "@gram/client/models/components/role.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import {
  Badge,
  Button,
  cn,
  Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Input,
  Separator,
  Table,
} from "@speakeasy-api/moonshine";
import { Ellipsis, Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { ChangeEvent } from "react";
import {
  MOCK_SHADOW_MCP_REQUESTS,
  MOCK_SHADOW_MCP_ROLES,
  MOCK_SHADOW_MCP_SERVER_LIST,
} from "./shadow-mcp-mock-data";
import type {
  ShadowMCPApprovalRequest,
  ShadowMCPEvidence,
  ShadowMCPMatchBreadth,
  ShadowMCPRoleOption,
  ShadowMCPServerListEntry,
} from "./shadow-mcp-types";
import {
  formatShortDate,
  getDecisionLabel,
  getMatchBreadthLabel,
  getMatchValue,
} from "./shadow-mcp-utils";

type ReviewAction = "approve" | "deny";
type RuleDecisionFilter = "all" | ShadowMCPServerListEntry["decision"];
type TextInputChangeEvent = ChangeEvent<HTMLInputElement | HTMLTextAreaElement>;

const RULE_DECISION_OPTIONS: {
  value: ShadowMCPServerListEntry["decision"];
  label: string;
  description: string;
}[] = [
  {
    value: "allowed",
    label: "Allow",
    description: "Make this Shadow MCP server available to selected roles.",
  },
  {
    value: "denied",
    label: "Deny",
    description:
      "Block this Shadow MCP server even if a broader allow applies.",
  },
];

const REVIEW_ACTION_OPTIONS: {
  value: ReviewAction;
  label: string;
  description: string;
}[] = [
  {
    value: "approve",
    label: "Approve",
    description: "Add an allow rule and optionally grant role access.",
  },
  {
    value: "deny",
    label: "Deny",
    description: "Add a deny rule that blocks this server.",
  },
];

function handleStringInputChange(onChange: (value: string) => void) {
  return (event: TextInputChangeEvent) => onChange(event.currentTarget.value);
}

function roleOptionsFromRoles(roles: Role[]): ShadowMCPRoleOption[] {
  if (roles.length === 0) return MOCK_SHADOW_MCP_ROLES;

  return roles.map((role) => ({
    id: role.id,
    name: role.name,
    description: role.description,
    isSystem: role.isSystem,
  }));
}

function EvidenceCell({ evidence }: { evidence: ShadowMCPEvidence }) {
  return (
    <div className="min-w-0 space-y-1">
      <Type variant="body" className="truncate font-medium">
        {evidence.name}
      </Type>
      <Type variant="body" className="text-muted-foreground truncate text-xs">
        {evidence.fullUrl}
      </Type>
    </div>
  );
}

function DecisionBadge({
  decision,
}: {
  decision: ShadowMCPServerListEntry["decision"];
}) {
  return (
    <Badge variant={decision === "allowed" ? "success" : "destructive"}>
      <Badge.Text>{getDecisionLabel(decision)}</Badge.Text>
    </Badge>
  );
}

function RoleBadges({
  roleIds,
  roles,
}: {
  roleIds: string[];
  roles: ShadowMCPRoleOption[];
}) {
  if (roleIds.length === 0) {
    return (
      <Type variant="body" className="text-muted-foreground text-sm">
        No role grants
      </Type>
    );
  }

  return (
    <div className="flex flex-wrap gap-1">
      {roleIds.map((roleId) => {
        const role = roles.find((r) => r.id === roleId);
        return (
          <Badge key={roleId} variant="neutral">
            <Badge.Text>{role?.name ?? roleId}</Badge.Text>
          </Badge>
        );
      })}
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
  const toggleRole = (roleId: string) => {
    if (selectedRoleIds.includes(roleId)) {
      onChange(selectedRoleIds.filter((id) => id !== roleId));
    } else {
      onChange([...selectedRoleIds, roleId]);
    }
  };

  return (
    <div className="border-border divide-border divide-y rounded-md border">
      {roles.map((role) => (
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
            <div className="flex items-center gap-2">
              <Type variant="body" className="text-sm font-medium">
                {role.name}
              </Type>
              {role.isSystem && (
                <Badge variant="neutral">
                  <Badge.Text>System</Badge.Text>
                </Badge>
              )}
            </div>
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

function EntryActionsMenu({
  entry,
  onEdit,
  onDelete,
}: {
  entry: ShadowMCPServerListEntry;
  onEdit: (entry: ShadowMCPServerListEntry) => void;
  onDelete: (entry: ShadowMCPServerListEntry) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <RequireScope scope="org:admin" level="component">
      <DropdownMenu open={open} onOpenChange={setOpen} modal={false}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className={cn(
              "text-muted-foreground hover:bg-accent hover:text-foreground flex h-8 w-8 cursor-pointer items-center justify-center rounded-md transition-colors",
              open && "bg-accent text-foreground",
            )}
          >
            <Ellipsis className="h-4 w-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => setTimeout(() => onEdit(entry), 0)}>
            Edit
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={() => setTimeout(() => onDelete(entry), 0)}
          >
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </RequireScope>
  );
}

function ReviewActionBadge({ action }: { action: ReviewAction }) {
  return (
    <Badge variant={action === "approve" ? "success" : "destructive"}>
      <Badge.Text>{action === "approve" ? "Approve" : "Deny"}</Badge.Text>
    </Badge>
  );
}

function ReviewRequestSheet({
  open,
  request,
  roles,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  request: ShadowMCPApprovalRequest | null;
  roles: ShadowMCPRoleOption[];
  onOpenChange: (open: boolean) => void;
  onSubmit: (input: {
    request: ShadowMCPApprovalRequest;
    action: ReviewAction;
    matchBreadth: ShadowMCPMatchBreadth;
    roleIds: string[];
    reason: string;
  }) => void;
}) {
  const [action, setAction] = useState<ReviewAction>("approve");
  const [matchBreadth, setMatchBreadth] =
    useState<ShadowMCPMatchBreadth>("full_url");
  const [roleIds, setRoleIds] = useState<string[]>([]);
  const [reason, setReason] = useState("");

  const isApprove = action === "approve";

  useEffect(() => {
    if (!open) return;
    setAction("approve");
    setMatchBreadth("full_url");
    setRoleIds([]);
    setReason("");
  }, [open, request]);

  const close = () => {
    setAction("approve");
    setMatchBreadth("full_url");
    setRoleIds([]);
    setReason("");
    onOpenChange(false);
  };

  return (
    <Sheet open={open} onOpenChange={(nextOpen) => !nextOpen && close()}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-lg"
      >
        <SheetHeader>
          <SheetTitle>Review Shadow MCP Request</SheetTitle>
          <SheetDescription>
            Approve or deny this blocked Shadow MCP server request.
          </SheetDescription>
        </SheetHeader>

        {request && (
          <div className="flex-1 space-y-4 overflow-y-auto px-4">
            <div className="space-y-2">
              <Type variant="body" className="text-sm font-medium">
                Decision
              </Type>
              <RadioGroup
                value={action}
                onValueChange={(value) => {
                  const nextAction = value as ReviewAction;
                  setAction(nextAction);
                  if (nextAction === "deny") {
                    setRoleIds([]);
                  }
                }}
              >
                <div className="border-border divide-border divide-y rounded-md border">
                  {REVIEW_ACTION_OPTIONS.map((option) => (
                    <label
                      key={option.value}
                      htmlFor={`shadow-mcp-review-${option.value}`}
                      className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 p-3"
                    >
                      <RadioGroupItem
                        id={`shadow-mcp-review-${option.value}`}
                        value={option.value}
                        className="mt-0.5"
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <ReviewActionBadge action={option.value} />
                          <Type variant="body" className="text-sm font-medium">
                            {option.label}
                          </Type>
                        </div>
                        <Type
                          variant="body"
                          className="text-muted-foreground mt-1 text-xs"
                        >
                          {option.description}
                        </Type>
                      </div>
                    </label>
                  ))}
                </div>
              </RadioGroup>
            </div>

            <div className="border-border bg-muted/30 rounded-md border p-3">
              <EvidenceCell evidence={request.evidence} />
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="space-y-2">
                <Type variant="body" className="text-sm font-medium">
                  Match breadth
                </Type>
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
                    <SelectItem
                      value="full_url"
                      description="Default. Matches this exact MCP endpoint."
                    >
                      Full URL
                    </SelectItem>
                    <SelectItem
                      value="url_host"
                      description="Broader. Matches all endpoints on the host."
                    >
                      URL host
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <Type variant="body" className="text-sm font-medium">
                  Match preview
                </Type>
                <div className="border-border bg-background flex h-9 items-center rounded-md border px-3">
                  <Type variant="body" className="truncate font-mono text-xs">
                    {matchBreadth === "full_url"
                      ? request.evidence.fullUrl
                      : request.evidence.urlHost}
                  </Type>
                </div>
              </div>
            </div>

            {isApprove && (
              <div className="space-y-2">
                <Type variant="body" className="text-sm font-medium">
                  Grant access to roles
                </Type>
                <RolePicker
                  roles={roles}
                  selectedRoleIds={roleIds}
                  onChange={setRoleIds}
                />
              </div>
            )}

            <div className="space-y-2">
              <Type variant="body" className="text-sm font-medium">
                Admin note
              </Type>
              <Input
                value={reason}
                onChange={handleStringInputChange(setReason)}
                placeholder={
                  isApprove
                    ? "Why is this server approved?"
                    : "Why is this server denied?"
                }
              />
            </div>
          </div>
        )}

        <SheetFooter className="border-border flex-row justify-end border-t">
          <Button variant="secondary" onClick={close}>
            Cancel
          </Button>
          <Button
            disabled={!request}
            onClick={() => {
              if (!request) return;
              onSubmit({ request, action, matchBreadth, roleIds, reason });
              close();
            }}
          >
            <Button.Text>{isApprove ? "Approve" : "Deny"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

function ServerEntrySheet({
  open,
  entry,
  roles,
  onOpenChange,
  onSubmit,
}: {
  open: boolean;
  entry: ShadowMCPServerListEntry | null;
  roles: ShadowMCPRoleOption[];
  onOpenChange: (open: boolean) => void;
  onSubmit: (entry: ShadowMCPServerListEntry) => void;
}) {
  const [decision, setDecision] =
    useState<ShadowMCPServerListEntry["decision"]>("allowed");
  const [name, setName] = useState("");
  const [fullUrl, setFullUrl] = useState("");
  const [urlHost, setUrlHost] = useState("");
  const [normalizedIdentity, setNormalizedIdentity] = useState("");
  const [matchBreadth, setMatchBreadth] =
    useState<ShadowMCPMatchBreadth>("full_url");
  const [roleIds, setRoleIds] = useState<string[]>([]);
  const [reason, setReason] = useState("");

  const isEditing = !!entry;

  useEffect(() => {
    if (!open) return;
    if (!entry) {
      setDecision("allowed");
      setName("");
      setFullUrl("");
      setUrlHost("");
      setNormalizedIdentity("");
      setMatchBreadth("full_url");
      setRoleIds([]);
      setReason("");
      return;
    }
    setDecision(entry.decision);
    setName(entry.evidence.name);
    setFullUrl(entry.evidence.fullUrl);
    setUrlHost(entry.evidence.urlHost);
    setNormalizedIdentity(entry.evidence.normalizedIdentity);
    setMatchBreadth(entry.matchBreadth);
    setRoleIds(entry.roleIds);
    setReason(entry.reason ?? "");
  }, [entry, open]);

  const close = () => {
    setDecision("allowed");
    setName("");
    setFullUrl("");
    setUrlHost("");
    setNormalizedIdentity("");
    setMatchBreadth("full_url");
    setRoleIds([]);
    setReason("");
    onOpenChange(false);
  };

  const canSave = name.trim() && fullUrl.trim() && urlHost.trim();

  return (
    <Sheet open={open} onOpenChange={(nextOpen) => !nextOpen && close()}>
      <SheetContent
        side="right"
        className="flex w-full flex-col overflow-hidden sm:max-w-lg"
      >
        <SheetHeader>
          <SheetTitle>
            {isEditing ? "Edit Shadow MCP Rule" : "Add Access Rule"}
          </SheetTitle>
          <SheetDescription>
            Maintain the managed access rules that role permissions use during
            Shadow MCP enforcement.
          </SheetDescription>
        </SheetHeader>

        <div className="flex-1 space-y-4 overflow-y-auto px-4">
          <div className="space-y-2">
            <Type variant="body" className="text-sm font-medium">
              Rule type
            </Type>
            <RadioGroup
              value={decision}
              onValueChange={(value) => {
                const nextDecision =
                  value as ShadowMCPServerListEntry["decision"];
                setDecision(nextDecision);
                if (nextDecision === "denied") {
                  setRoleIds([]);
                }
              }}
            >
              <div className="border-border divide-border divide-y rounded-md border">
                {RULE_DECISION_OPTIONS.map((option) => (
                  <label
                    key={option.value}
                    htmlFor={`shadow-mcp-rule-${option.value}`}
                    className="hover:bg-muted/50 flex cursor-pointer items-start gap-3 p-3"
                  >
                    <RadioGroupItem
                      id={`shadow-mcp-rule-${option.value}`}
                      value={option.value}
                      className="mt-0.5"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <DecisionBadge decision={option.value} />
                        <Type variant="body" className="text-sm font-medium">
                          {option.label}
                        </Type>
                      </div>
                      <Type
                        variant="body"
                        className="text-muted-foreground mt-1 text-xs"
                      >
                        {option.description}
                      </Type>
                    </div>
                  </label>
                ))}
              </div>
            </RadioGroup>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="space-y-2">
              <Type variant="body" className="text-sm font-medium">
                Server name
              </Type>
              <Input
                value={name}
                onChange={handleStringInputChange(setName)}
                placeholder="linear"
              />
            </div>
            <div className="space-y-2">
              <Type variant="body" className="text-sm font-medium">
                Normalized identity
              </Type>
              <Input
                value={normalizedIdentity}
                onChange={handleStringInputChange(setNormalizedIdentity)}
                placeholder="linear"
              />
            </div>
            <div className="space-y-2 sm:col-span-2">
              <Type variant="body" className="text-sm font-medium">
                Full URL
              </Type>
              <Input
                value={fullUrl}
                onChange={(event) => {
                  const value = event.currentTarget.value;
                  setFullUrl(value);
                  try {
                    setUrlHost(new URL(value).host);
                  } catch {
                    // Keep manual host edits when URL is incomplete.
                  }
                }}
                placeholder="https://mcp.example.com/sse"
              />
            </div>
            <div className="space-y-2">
              <Type variant="body" className="text-sm font-medium">
                URL host
              </Type>
              <Input
                value={urlHost}
                onChange={handleStringInputChange(setUrlHost)}
                placeholder="mcp.example.com"
              />
            </div>
            <div className="space-y-2">
              <Type variant="body" className="text-sm font-medium">
                Match breadth
              </Type>
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
                  <SelectItem value="full_url">Full URL</SelectItem>
                  <SelectItem value="url_host">URL host</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          {decision === "allowed" && (
            <div className="space-y-2">
              <Type variant="body" className="text-sm font-medium">
                Grant access to roles
              </Type>
              <RolePicker
                roles={roles}
                selectedRoleIds={roleIds}
                onChange={setRoleIds}
              />
            </div>
          )}

          <div className="space-y-2">
            <Type variant="body" className="text-sm font-medium">
              Admin note
            </Type>
            <Input
              value={reason}
              onChange={handleStringInputChange(setReason)}
              placeholder="Reason"
            />
          </div>
        </div>

        <SheetFooter className="border-border flex-row justify-end border-t">
          <Button variant="secondary" onClick={close}>
            Cancel
          </Button>
          <Button
            disabled={!canSave}
            onClick={() => {
              const nextEntry: ShadowMCPServerListEntry = {
                id:
                  entry?.id ??
                  `entry-${normalizedIdentity || name}-${Date.now()}`,
                decision,
                evidence: {
                  name,
                  fullUrl,
                  urlHost,
                  normalizedIdentity: normalizedIdentity || name,
                },
                matchBreadth,
                roleIds: decision === "allowed" ? roleIds : [],
                createdAt: entry?.createdAt ?? new Date().toISOString(),
                createdBy: entry?.createdBy ?? "Current Admin",
                sourceRequestId: entry?.sourceRequestId,
                reason,
              };
              onSubmit(nextEntry);
              close();
            }}
          >
            <Button.Text>{isEditing ? "Save Changes" : "Add Rule"}</Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

export function ShadowMCPAccessContent() {
  const { data: rolesData } = useRoles();
  const roles = useMemo(
    () => roleOptionsFromRoles(rolesData?.roles ?? []),
    [rolesData?.roles],
  );
  const [requests, setRequests] = useState(MOCK_SHADOW_MCP_REQUESTS);
  const [entries, setEntries] = useState(MOCK_SHADOW_MCP_SERVER_LIST);
  const [reviewRequest, setReviewRequest] =
    useState<ShadowMCPApprovalRequest | null>(null);
  const [reviewSheetOpen, setReviewSheetOpen] = useState(false);
  const [entrySheetOpen, setEntrySheetOpen] = useState(false);
  const [ruleDecisionFilter, setRuleDecisionFilter] =
    useState<RuleDecisionFilter>("all");
  const [editingEntry, setEditingEntry] =
    useState<ShadowMCPServerListEntry | null>(null);

  const allowedEntries = entries.filter((e) => e.decision === "allowed");
  const deniedEntries = entries.filter((e) => e.decision === "denied");
  const pendingRequests = requests.filter((r) => r.status === "requested");
  const filteredEntries =
    ruleDecisionFilter === "all"
      ? entries
      : entries.filter((entry) => entry.decision === ruleDecisionFilter);

  const openReview = (request: ShadowMCPApprovalRequest) => {
    setReviewRequest(request);
    setReviewSheetOpen(true);
  };

  const openCreateRule = () => {
    setEditingEntry(null);
    setEntrySheetOpen(true);
  };

  const openEditRule = (entry: ShadowMCPServerListEntry) => {
    setEditingEntry(entry);
    setEntrySheetOpen(true);
  };

  const upsertEntry = (nextEntry: ShadowMCPServerListEntry) => {
    setEntries((prev) => {
      const exists = prev.some((entry) => entry.id === nextEntry.id);
      if (exists) {
        return prev.map((entry) =>
          entry.id === nextEntry.id ? nextEntry : entry,
        );
      }
      return [nextEntry, ...prev];
    });
  };

  const requestColumns: Column<ShadowMCPApprovalRequest>[] = [
    {
      key: "name",
      header: "Name",
      width: "1.2fr",
      render: (request) => <EvidenceCell evidence={request.evidence} />,
    },
    {
      key: "requester",
      header: "Requester",
      width: "1fr",
      render: (request) => (
        <div className="min-w-0">
          <Type variant="body" className="truncate font-medium">
            {request.requester.name}
          </Type>
          <Type
            variant="body"
            className="text-muted-foreground truncate text-xs"
          >
            {request.requester.email}
          </Type>
        </div>
      ),
    },
    {
      key: "context",
      header: "Context",
      width: "1fr",
      render: (request) => (
        <div className="min-w-0 space-y-1">
          <Type variant="body" className="truncate text-sm">
            {request.projectName}
          </Type>
          <Type
            variant="body"
            className="text-muted-foreground truncate font-mono text-xs"
          >
            {request.toolCall}
          </Type>
        </div>
      ),
    },
    {
      key: "activity",
      header: "Activity",
      width: "0.5fr",
      render: (request) => (
        <div>
          <Type variant="body" className="text-sm">
            {request.blockedCount} blocks
          </Type>
          <Type variant="body" className="text-muted-foreground text-xs">
            {formatShortDate(request.lastBlockedAt)}
          </Type>
        </div>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "120px",
      render: (request) =>
        request.status === "requested" ? (
          <RequireScope scope="org:admin" level="component">
            <div className="flex justify-end">
              <Button size="sm" onClick={() => openReview(request)}>
                <Button.Text>Review</Button.Text>
              </Button>
            </div>
          </RequireScope>
        ) : null,
    },
  ];

  const entryColumns: Column<ShadowMCPServerListEntry>[] = [
    {
      key: "status",
      header: "Status",
      width: "100px",
      render: (entry) => <DecisionBadge decision={entry.decision} />,
    },
    {
      key: "name",
      header: "Name",
      width: "1.2fr",
      render: (entry) => <EvidenceCell evidence={entry.evidence} />,
    },
    {
      key: "match",
      header: "Match Rule",
      width: "1fr",
      render: (entry) => (
        <div className="min-w-0 space-y-1">
          <Badge variant="neutral">
            <Badge.Text>{getMatchBreadthLabel(entry.matchBreadth)}</Badge.Text>
          </Badge>
          <Type
            variant="body"
            className="text-muted-foreground truncate font-mono text-xs"
          >
            {getMatchValue(entry)}
          </Type>
        </div>
      ),
    },
    {
      key: "roles",
      header: "Role grants",
      width: "1.1fr",
      render: (entry) => <RoleBadges roleIds={entry.roleIds} roles={roles} />,
    },
    {
      key: "created",
      header: "Created",
      width: "0.6fr",
      render: (entry) => (
        <div>
          <Type variant="body" className="text-sm">
            {formatShortDate(entry.createdAt)}
          </Type>
          <Type variant="body" className="text-muted-foreground text-xs">
            {entry.createdBy}
          </Type>
        </div>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "96px",
      render: (entry) => (
        <div className="flex justify-end">
          <EntryActionsMenu
            entry={entry}
            onEdit={openEditRule}
            onDelete={(target) =>
              setEntries((prev) =>
                prev.filter((entry) => entry.id !== target.id),
              )
            }
          />
        </div>
      ),
    },
  ];

  return (
    <div className="space-y-8">
      <div className="flex items-start justify-between gap-4">
        <div>
          <Heading variant="h4">Shadow MCP</Heading>
          <Type muted small className="mt-1 max-w-3xl">
            Review blocked Shadow MCP access requests, maintain managed access
            rules, and grant approved external servers to roles through MCP
            connect permissions.
          </Type>
        </div>
      </div>

      <Separator />

      <div className="space-y-12">
        <section className="space-y-4">
          <div>
            <Heading variant="h5">Requests</Heading>
            <Type muted small className="mt-1">
              Shadow MCP servers waiting for admin review.
            </Type>
          </div>
          <Table
            columns={requestColumns}
            data={pendingRequests}
            rowKey={(row) => row.id}
            className="[&_thead]:bg-background max-h-128 overflow-y-auto [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
          />
        </section>

        <section className="space-y-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <Heading variant="h5">Access Rules</Heading>
              <Type muted small className="mt-1">
                Allow and deny access rules available for Shadow MCP role
                permissions.
              </Type>
            </div>
            <div className="flex shrink-0 flex-wrap justify-end gap-2">
              <Select
                value={ruleDecisionFilter}
                onValueChange={(value) =>
                  setRuleDecisionFilter(value as RuleDecisionFilter)
                }
              >
                <SelectTrigger className="w-[150px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All access rules</SelectItem>
                  <SelectItem value="allowed">
                    Allowed ({allowedEntries.length})
                  </SelectItem>
                  <SelectItem value="denied">
                    Denied ({deniedEntries.length})
                  </SelectItem>
                </SelectContent>
              </Select>
              <RequireScope scope="org:admin" level="component">
                <Button onClick={openCreateRule} size="md" variant="secondary">
                  <Button.LeftIcon>
                    <Plus className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>Add Access Rule</Button.Text>
                </Button>
              </RequireScope>
            </div>
          </div>
          <Table
            columns={entryColumns}
            data={filteredEntries}
            rowKey={(row) => row.id}
            className="[&_thead]:bg-background max-h-128 overflow-y-auto [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
          />
        </section>
      </div>

      <ReviewRequestSheet
        open={reviewSheetOpen}
        request={reviewRequest}
        roles={roles}
        onOpenChange={(open) => {
          setReviewSheetOpen(open);
          if (!open) {
            setReviewRequest(null);
          }
        }}
        onSubmit={({ request, action, matchBreadth, roleIds, reason }) => {
          const decision = action === "approve" ? "allowed" : "denied";
          upsertEntry({
            id: `entry-${request.id}-${decision}`,
            decision,
            evidence: request.evidence,
            matchBreadth,
            roleIds: decision === "allowed" ? roleIds : [],
            createdAt: new Date().toISOString(),
            createdBy: "Current Admin",
            sourceRequestId: request.id,
            reason,
          });
          setRequests((prev) =>
            prev.map((item) =>
              item.id === request.id
                ? {
                    ...item,
                    status: action === "approve" ? "approved" : "denied",
                  }
                : item,
            ),
          );
        }}
      />

      <ServerEntrySheet
        open={entrySheetOpen}
        entry={editingEntry}
        roles={roles}
        onOpenChange={(open) => {
          setEntrySheetOpen(open);
          if (!open) {
            setEditingEntry(null);
          }
        }}
        onSubmit={upsertEntry}
      />
    </div>
  );
}
