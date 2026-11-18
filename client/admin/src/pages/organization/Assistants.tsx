import { useMemo, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";

import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import {
  organizationAssistantsQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import {
  type AdminAssistantIdentityState,
  type AdminOrganization,
  type AdminOrganizationAssistant,
} from "@/lib/gramAdminApi";
import { fmtDateShort } from "@/lib/utils";

const assistantColumn = createColumnHelper<
  DataTableFeatures,
  AdminOrganizationAssistant
>();

const IDENTITY_LABELS: Record<AdminAssistantIdentityState, string> = {
  NEVER_CONFIGURED: "None",
  ACTIVE: "Agent",
  UNAVAILABLE: "Unavailable",
};

const RUNTIME_LABELS: Record<number, string> = { 2: "Default", 3: "Kit" };

function assistantsMessage(isPending: boolean, isError: boolean): string {
  if (isPending) return "Loading...";
  if (isError) return "Unable to load assistants";
  return "No assistants in this organization";
}

function IdentityBadge({
  state,
}: {
  state: AdminAssistantIdentityState;
}): JSX.Element {
  return (
    <Badge
      variant={
        state === "ACTIVE"
          ? "secondary"
          : state === "UNAVAILABLE"
            ? "destructive"
            : "outline"
      }
    >
      {IDENTITY_LABELS[state]}
    </Badge>
  );
}

function assistantColumns() {
  return assistantColumn.columns([
    assistantColumn.accessor("name", {
      header: "Name",
      meta: { cellClassName: "whitespace-normal" },
      cell: ({ row }) => <span className="text-sm">{row.original.name}</span>,
    }),
    assistantColumn.accessor("project_name", {
      header: "Project",
      meta: { cellClassName: "whitespace-normal" },
      cell: ({ row }) => (
        <span className="text-sm">{row.original.project_name}</span>
      ),
    }),
    assistantColumn.accessor("identity_state", {
      header: "Identity",
      cell: ({ row }) => <IdentityBadge state={row.original.identity_state} />,
    }),
    assistantColumn.accessor("runtime_version", {
      header: "Runtime",
      cell: ({ row }) => (
        <span className="text-sm">
          {RUNTIME_LABELS[row.original.runtime_version] ??
            String(row.original.runtime_version)}
        </span>
      ),
    }),
    assistantColumn.accessor("created_by_user_email", {
      header: "Creator",
      meta: { cellClassName: "whitespace-normal break-all" },
      cell: ({ row }) => (
        <span className="text-sm">
          {row.original.created_by_user_email ?? "Unknown"}
          {!row.original.creator_active && (
            <span className="text-muted-foreground"> (not a member)</span>
          )}
        </span>
      ),
    }),
    assistantColumn.accessor("created_at", {
      header: "Created",
      cell: ({ row }) => (
        <span className="text-sm">{fmtDateShort(row.original.created_at)}</span>
      ),
    }),
  ]);
}

export function AssistantsRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Assistants org={data} />;
}

export function Assistants({ org }: { org: AdminOrganization }): JSX.Element {
  const query = organizationAssistantsQuery(org.id);
  const { data, isPending, isError } = useQuery({
    ...query,
    enabled: !!org.id,
  });
  const assistants = useMemo(() => data?.assistants ?? [], [data]);
  const unmigrated = assistants.filter(
    (assistant) => assistant.identity_state === "NEVER_CONFIGURED",
  );
  const columns = assistantColumns();
  const table = useTable({
    features: dataTableFeatures,
    columns,
    data: assistants,
    getRowId: (assistant) => assistant.id,
  });
  const rows = table.getRowModel().rows;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        {rows.length > 0 && (
          <p className="text-sm font-medium">
            {rows.length === 1 ? "1 assistant" : `${rows.length} assistants`},{" "}
            {unmigrated.length} without an agent identity
          </p>
        )}
        <Link
          to="/assistants"
          search={{ organization: org.id }}
          className="ml-auto text-sm underline-offset-4 hover:underline focus-visible:underline"
        >
          Manage assistant migrations
        </Link>
      </div>
      <div
        tabIndex={-1}
        role="group"
        aria-label="Assistants"
        className="focus-visible:ring-ring overflow-clip rounded-lg border focus-visible:ring-2 focus-visible:outline-none"
      >
        <Table cellPadding="condensed">
          <Table.Header table={table} />
          <Table.Body>
            {isPending || rows.length === 0 ? (
              <Table.NoResultsMessage>
                <span className="text-muted-foreground text-sm">
                  {assistantsMessage(isPending, isError)}
                </span>
              </Table.NoResultsMessage>
            ) : (
              rows.map((row) => <Table.Row key={row.id} row={row} />)
            )}
          </Table.Body>
        </Table>
      </div>
    </div>
  );
}
