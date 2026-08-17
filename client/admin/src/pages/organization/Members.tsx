import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";

import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import {
  organizationMembersQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import type {
  AdminOrganization,
  AdminOrganizationMember,
} from "@/lib/gramAdminApi";
import { cn, fmtDateShort } from "@/lib/utils";

const memberColumn = createColumnHelper<
  DataTableFeatures,
  AdminOrganizationMember
>();

const MEMBER_COLUMNS = memberColumn.columns([
  memberColumn.accessor("email", {
    header: "Email",
    cell: ({ row }) => <span className="text-sm">{row.original.email}</span>,
  }),
  memberColumn.accessor("display_name", {
    header: "Name",
    cell: ({ row }) => (
      <span className="text-sm">{row.original.display_name}</span>
    ),
  }),
  memberColumn.accessor("id", {
    header: "ID",
    cell: ({ row }) => (
      <span className="text-muted-foreground text-sm">{row.original.id}</span>
    ),
  }),
  memberColumn.accessor("last_login", {
    header: "Last login",
    cell: ({ row }) => (
      <span
        className={cn(
          "text-sm",
          !row.original.last_login && "text-muted-foreground",
        )}
      >
        {fmtDateShort(row.original.last_login)}
      </span>
    ),
  }),
  memberColumn.accessor("created_at", {
    header: "Joined",
    cell: ({ row }) => (
      <span className="text-sm">{fmtDateShort(row.original.created_at)}</span>
    ),
  }),
]);

// A fresh fallback array each render would rebuild the row model every time.
const NO_MEMBERS: AdminOrganizationMember[] = [];

// `isPending`, not `isLoading`: React Query makes the second of those
// `isPending && isFetching`, so a paused read is neither loading nor errored and
// falls through to the sentence that says the organization has none.
function membersMessage(isPending: boolean, isError: boolean): string {
  if (isPending) return "Loading...";
  if (isError) return "Unable to load members";
  return "No members in this organization";
}

export function MembersRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Members org={data} />;
}

export function Members({ org }: { org: AdminOrganization }): JSX.Element {
  const { data, isPending, isError } = useQuery({
    ...organizationMembersQuery(org.id),
    enabled: !!org.id,
  });

  const table = useTable({
    features: dataTableFeatures,
    columns: MEMBER_COLUMNS,
    data: data?.members ?? NO_MEMBERS,
    getRowId: (member) => member.id,
  });

  const rows = table.getRowModel().rows;

  return (
    <div className="max-h-96 overflow-auto rounded-lg border">
      <Table cellPadding="condensed">
        <Table.Header table={table} />
        <Table.Body>
          {isPending || rows.length === 0 ? (
            <Table.NoResultsMessage>
              <span className="text-muted-foreground text-sm">
                {membersMessage(isPending, isError)}
              </span>
            </Table.NoResultsMessage>
          ) : (
            rows.map((row) => <Table.Row key={row.id} row={row} />)
          )}
        </Table.Body>
      </Table>
    </div>
  );
}
