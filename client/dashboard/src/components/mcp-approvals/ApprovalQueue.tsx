import { StatusBadge } from "@/components/mcp-approvals/EvidencePanel";
import { defineFilters, useFilterState } from "@/components/filters";
import type { FilterValue } from "@/components/filters";
import { Page } from "@/components/page-layout";
import { Column, Table } from "@/components/ui/Table";
import { useProject } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import type { ApprovalRequestSummary } from "@gram/client/models/components/approvalrequestsummary.js";
import { useListMcpApprovalRequests } from "@gram/client/react-query/listMcpApprovalRequests.js";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router";

const STATUS_OPTIONS = [
  { value: "requested", label: "Awaiting decision" },
  { value: "approved", label: "Approved" },
  { value: "denied", label: "Denied" },
];

const FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "select" },
]);

function ServerCell({
  request,
}: {
  request: ApprovalRequestSummary;
}): JSX.Element {
  return (
    <div className="min-w-0">
      <p className="truncate font-medium">{request.targetRaw}</p>
      {request.artifactRef ? (
        <p className="text-muted-foreground truncate text-xs">
          {request.artifactRef}
        </p>
      ) : (
        <p className="text-muted-foreground text-xs italic">
          Could not be identified
        </p>
      )}
    </div>
  );
}

/**
 * The approval request queue: rendered as tab content inside the MCP page,
 * not a page of its own. What needs deciding surfaces first; decided history
 * sits below it.
 */
export function ApprovalQueue(): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const { values, setValue, clearValue, clearAll } = useFilterState(FILTERS);
  const status = values.status;

  const listQuery = useListMcpApprovalRequests(
    { gramProject: project.slug, status: status ?? undefined },
    undefined,
    { enabled: project.slug.length > 0 },
  );

  const requests = useMemo(() => {
    const rows = listQuery.data?.requests ?? [];
    const needle = search.trim().toLowerCase();
    const matched =
      needle.length === 0
        ? rows
        : rows.filter(
            (row) =>
              row.targetRaw.toLowerCase().includes(needle) ||
              (row.artifactRef ?? "").toLowerCase().includes(needle),
          );

    return [...matched].sort((a, b) => {
      const aPending = a.status === "requested" ? 0 : 1;
      const bPending = b.status === "requested" ? 0 : 1;
      if (aPending !== bPending) return aPending - bPending;
      return b.updatedAt.getTime() - a.updatedAt.getTime();
    });
  }, [listQuery.data, search]);

  const columns: Column<ApprovalRequestSummary>[] = [
    {
      key: "server",
      header: "Server",
      render: (request) => <ServerCell request={request} />,
    },
    {
      key: "status",
      header: "Status",
      width: "160px",
      render: (request) => <StatusBadge status={request.status} />,
    },
    {
      key: "requesters",
      header: "Requesters",
      width: "110px",
      render: (request) => <span>{request.requesterCount}</span>,
    },
    {
      key: "updated",
      header: "Updated",
      width: "160px",
      render: (request) => (
        <span className="text-muted-foreground text-sm">
          <HumanizeDateTime date={request.updatedAt} />
        </span>
      ),
    },
  ];

  return (
    <>
      <Page.Toolbar>
        <Page.Toolbar.Search
          value={search}
          onChange={setSearch}
          placeholder="Search by server or artifact"
          debounceMs={200}
        />
        <Page.Toolbar.Filters
          schema={FILTERS}
          values={values}
          optionsById={{ status: STATUS_OPTIONS }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
      </Page.Toolbar>
      <Table
        columns={columns}
        data={requests}
        rowKey={(row) => row.id}
        onRowClick={(row) =>
          void navigate(routes.mcp.approvalRequest.href(row.id))
        }
        noResultsMessage={<span>No matching requests.</span>}
      />
    </>
  );
}
