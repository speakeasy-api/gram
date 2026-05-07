import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import { Heading } from "@/components/ui/heading";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import type { Role } from "@gram/client/models/components/role.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import {
  Badge,
  Button,
  cn,
  Column,
  Dialog,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Input,
  Separator,
  Table,
} from "@speakeasy-api/moonshine";
import { Check, Ellipsis, ShieldCheck, ShieldX, X } from "lucide-react";
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
  getShadowMCPSummary,
} from "./shadow-mcp-utils";

type ReviewAction = "approve" | "deny";
type RuleDecisionFilter = "all" | ShadowMCPServerListEntry["decision"];
type TextInputChangeEvent = ChangeEvent<HTMLInputElement | HTMLTextAreaElement>;

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
      <div className="flex min-w-0 items-center gap-2">
        <Type variant="body" className="truncate font-medium">
          {evidence.name}
        </Type>
        <Badge variant="neutral" className="font-mono">
          <Badge.Text>{evidence.normalizedIdentity}</Badge.Text>
        </Badge>
      </div>
      <Type variant="body" className="text-muted-foreground truncate text-xs">
        {evidence.fullUrl}
      </Type>
      <Type variant="body" className="text-muted-foreground truncate text-xs">
        Host: {evidence.urlHost}
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

function ReviewRequestDialog({
  request,
  action,
  roles,
  onOpenChange,
  onSubmit,
}: {
  request: ShadowMCPApprovalRequest | null;
  action: ReviewAction | null;
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
  const [matchBreadth, setMatchBreadth] =
    useState<ShadowMCPMatchBreadth>("full_url");
  const [roleIds, setRoleIds] = useState<string[]>([]);
  const [reason, setReason] = useState("");

  const open = !!request && !!action;
  const isApprove = action === "approve";

  const close = () => {
    setMatchBreadth("full_url");
    setRoleIds([]);
    setReason("");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && close()}>
      <Dialog.Content className="sm:max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>
            {isApprove ? "Approve Shadow MCP Server" : "Deny Shadow MCP Server"}
          </Dialog.Title>
          <Dialog.Description>
            {isApprove
              ? "Approving adds this server to the allowed list and can grant roles access to connect."
              : "Denying adds this server to the denied list. Denied entries override role grants."}
          </Dialog.Description>
        </Dialog.Header>

        {request && action && (
          <div className="space-y-4">
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

        <Dialog.Footer>
          <Button variant="secondary" onClick={close}>
            Cancel
          </Button>
          <Button
            disabled={!request || !action}
            onClick={() => {
              if (!request || !action) return;
              onSubmit({ request, action, matchBreadth, roleIds, reason });
              close();
            }}
          >
            <Button.LeftIcon>
              {isApprove ? (
                <Check className="h-4 w-4" />
              ) : (
                <X className="h-4 w-4" />
              )}
            </Button.LeftIcon>
            <Button.Text>{isApprove ? "Approve" : "Deny"}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

function ServerEntryDialog({
  entry,
  decision,
  roles,
  onOpenChange,
  onSubmit,
}: {
  entry: ShadowMCPServerListEntry | null;
  decision: ShadowMCPServerListEntry["decision"] | null;
  roles: ShadowMCPRoleOption[];
  onOpenChange: (open: boolean) => void;
  onSubmit: (entry: ShadowMCPServerListEntry) => void;
}) {
  const [name, setName] = useState("");
  const [fullUrl, setFullUrl] = useState("");
  const [urlHost, setUrlHost] = useState("");
  const [normalizedIdentity, setNormalizedIdentity] = useState("");
  const [matchBreadth, setMatchBreadth] =
    useState<ShadowMCPMatchBreadth>("full_url");
  const [roleIds, setRoleIds] = useState<string[]>([]);
  const [reason, setReason] = useState("");

  const open = !!decision || !!entry;
  const effectiveDecision = entry?.decision ?? decision ?? "allowed";
  const isEditing = !!entry;

  useEffect(() => {
    if (!entry || !open) return;
    setName(entry.evidence.name);
    setFullUrl(entry.evidence.fullUrl);
    setUrlHost(entry.evidence.urlHost);
    setNormalizedIdentity(entry.evidence.normalizedIdentity);
    setMatchBreadth(entry.matchBreadth);
    setRoleIds(entry.roleIds);
    setReason(entry.reason ?? "");
  }, [entry, open]);

  const close = () => {
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
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && close()}>
      <Dialog.Content className="sm:max-w-2xl">
        <Dialog.Header>
          <Dialog.Title>
            {isEditing
              ? "Edit Shadow MCP Rule"
              : effectiveDecision === "allowed"
                ? "Add Allow Rule"
                : "Add Deny Rule"}
          </Dialog.Title>
          <Dialog.Description>
            Maintain the managed access rules that role permissions use during
            Shadow MCP enforcement.
          </Dialog.Description>
        </Dialog.Header>

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

        {effectiveDecision === "allowed" && (
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

        <Dialog.Footer>
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
                decision: effectiveDecision,
                evidence: {
                  name,
                  fullUrl,
                  urlHost,
                  normalizedIdentity: normalizedIdentity || name,
                },
                matchBreadth,
                roleIds: effectiveDecision === "allowed" ? roleIds : [],
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
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
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
  const [reviewAction, setReviewAction] = useState<ReviewAction | null>(null);
  const [entryDialogDecision, setEntryDialogDecision] = useState<
    ShadowMCPServerListEntry["decision"] | null
  >(null);
  const [ruleDecisionFilter, setRuleDecisionFilter] =
    useState<RuleDecisionFilter>("all");
  const [editingEntry, setEditingEntry] =
    useState<ShadowMCPServerListEntry | null>(null);

  const summary = getShadowMCPSummary({ requests, entries });
  const allowedEntries = entries.filter((e) => e.decision === "allowed");
  const deniedEntries = entries.filter((e) => e.decision === "denied");
  const pendingRequests = requests.filter((r) => r.status === "requested");
  const filteredEntries =
    ruleDecisionFilter === "all"
      ? entries
      : entries.filter((entry) => entry.decision === ruleDecisionFilter);

  const openReview = (
    request: ShadowMCPApprovalRequest,
    action: ReviewAction,
  ) => {
    setReviewRequest(request);
    setReviewAction(action);
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
      key: "server",
      header: "Server",
      width: "1.4fr",
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
      width: "130px",
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
      width: "170px",
      render: (request) =>
        request.status === "requested" ? (
          <RequireScope scope="org:admin" level="component">
            <div className="flex justify-end gap-2">
              <Button
                variant="secondary"
                size="sm"
                onClick={() => openReview(request, "deny")}
              >
                <Button.Text>Deny</Button.Text>
              </Button>
              <Button size="sm" onClick={() => openReview(request, "approve")}>
                <Button.Text>Approve</Button.Text>
              </Button>
            </div>
          </RequireScope>
        ) : null,
    },
  ];

  const entryColumns: Column<ShadowMCPServerListEntry>[] = [
    {
      key: "server",
      header: "Server",
      width: "1.4fr",
      render: (entry) => <EvidenceCell evidence={entry.evidence} />,
    },
    {
      key: "decision",
      header: "Rule",
      width: "100px",
      render: (entry) => <DecisionBadge decision={entry.decision} />,
    },
    {
      key: "match",
      header: "Match",
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
      width: "150px",
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
      width: "70px",
      render: (entry) => (
        <div className="flex justify-end">
          <EntryActionsMenu
            entry={entry}
            onEdit={setEditingEntry}
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
                <Button
                  variant="secondary"
                  onClick={() => setEntryDialogDecision("denied")}
                >
                  <Button.LeftIcon>
                    <ShieldX className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>Add Deny Rule</Button.Text>
                </Button>
                <Button onClick={() => setEntryDialogDecision("allowed")}>
                  <Button.LeftIcon>
                    <ShieldCheck className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>Add Allow Rule</Button.Text>
                </Button>
              </RequireScope>
            </div>
          </div>
          <Table
            columns={entryColumns}
            data={filteredEntries}
            rowKey={(row) => row.id}
          />
        </section>
      </div>

      <ReviewRequestDialog
        request={reviewRequest}
        action={reviewAction}
        roles={roles}
        onOpenChange={(open) => {
          if (!open) {
            setReviewRequest(null);
            setReviewAction(null);
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

      <ServerEntryDialog
        entry={editingEntry}
        decision={entryDialogDecision}
        roles={roles}
        onOpenChange={(open) => {
          if (!open) {
            setEditingEntry(null);
            setEntryDialogDecision(null);
          }
        }}
        onSubmit={upsertEntry}
      />
    </div>
  );
}
