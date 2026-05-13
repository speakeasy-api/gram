import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
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
import type { ShadowMCPAccessRule } from "@gram/client/models/components/shadowmcpaccessrule.js";
import type { ShadowMCPApprovalRequest } from "@gram/client/models/components/shadowmcpapprovalrequest.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import { useShadowMCPAccessRules } from "@gram/client/react-query/shadowMCPAccessRules.js";
import { useShadowMCPApprovalRequests } from "@gram/client/react-query/shadowMCPApprovalRequests.js";
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
import { Ellipsis, Plus } from "lucide-react";
import type React from "react";
import { useMemo, useState } from "react";
import {
  formatShortDate,
  getDispositionLabel,
  getMatchBreadthLabel,
  getRequesterDetail,
  getRequesterLabel,
  getRequestDisplayName,
  getRequestStatusLabel,
  getRuleDisplayName,
  roleNamesForIds,
  roleOptionsFromRoles,
} from "./shadow-mcp-utils";

type RequestStatusFilter = "requested" | "approved" | "denied" | "all";
type RuleDispositionFilter = "allowed" | "denied" | "all";

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
  const [requestStatusFilter, setRequestStatusFilter] =
    useState<RequestStatusFilter>("requested");
  const [ruleDispositionFilter, setRuleDispositionFilter] =
    useState<RuleDispositionFilter>("all");

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
      render: () => (
        <RequireScope scope="org:admin" level="component">
          <Button size="sm" disabled>
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
      render: () => (
        <RuleActionsMenu onEdit={() => undefined} onDelete={() => undefined} />
      ),
    },
  ];

  return (
    <div className="space-y-8">
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
              <Button disabled>
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
