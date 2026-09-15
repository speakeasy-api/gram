import { Link } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";

import { selectColumn, type DataTableFeatures } from "@/components/data-table";
import { Trial } from "@/components/Trial";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { cn, fmtDateShort } from "@/lib/utils";

import { OrganizationActions } from "./OrganizationActions";
import { PeekTrigger } from "./PeekTrigger";

const column = createColumnHelper<DataTableFeatures, AdminOrganization>();

// `w-px` shrinks the column to its content, so the pin does not read as a
// gutter. The header row is already `z-10`, so this stays under it.
const PINNED_RIGHT = "sticky right-0 z-1 w-px";

// Module scope gives the array one identity for the life of the tab, so the
// table does not rebuild its column model on every render. The header of each
// column doubles as its label in the Columns control.
export const ORG_COLUMNS = column.columns([
  // Leftmost, so the checkbox is the first thing on the row and the first tab
  // stop in it. The bulk actions above the table are the only thing that reads
  // the selection, and this is the only way to make one.
  selectColumn<AdminOrganization>({
    allLabel: "Select every organization on this page",
    rowLabel: (org) => `Select ${org.name}`,
  }),
  column.accessor("name", {
    header: "Name",
    // The link, not the row, carries the keyboard path and the accessible
    // name. It also lets the operator open the organization in a new tab.
    cell: ({ row }) => (
      <Link
        to="/organizations/$idOrSlug"
        params={{ idOrSlug: row.original.slug || row.original.id }}
        className="text-sm underline-offset-4 hover:underline focus-visible:underline"
      >
        {row.original.name}
      </Link>
    ),
  }),
  column.accessor("slug", {
    header: "Slug",
    cell: ({ row }) => <span className="text-sm">{row.original.slug}</span>,
  }),
  column.accessor("account_type", {
    header: "Type",
    cell: ({ row }) => (
      <Badge variant="outline" className={badgeTone.neutral}>
        {row.original.account_type}
      </Badge>
    ),
  }),
  column.accessor("member_count", {
    header: "Members",
    cell: ({ row }) => (
      <span className="text-sm">{row.original.member_count}</span>
    ),
  }),
  column.accessor("workos_id", {
    header: "WorkOS",
    cell: ({ row }) => (
      <span
        className={cn(
          "text-sm",
          !row.original.workos_id && "text-muted-foreground",
        )}
      >
        {row.original.workos_id
          ? `${row.original.workos_id.substring(0, 12)}...`
          : "-"}
      </span>
    ),
  }),
  column.accessor("disabled_at", {
    header: "Disabled",
    cell: ({ row }) => (
      <span
        className={cn(
          "text-sm",
          !row.original.disabled_at && "text-muted-foreground",
        )}
      >
        {row.original.disabled_at
          ? fmtDateShort(row.original.disabled_at)
          : "-"}
      </span>
    ),
  }),
  // "Trial", not "Trial ends": the cell reads as a state, and the end date is
  // the detail underneath it rather than the column's subject.
  column.accessor("trial_state", {
    header: "Trial",
    cell: ({ row }) => <Trial org={row.original} />,
  }),
  column.accessor("created_at", {
    header: "Created",
    cell: ({ row }) => (
      <span className="text-sm">{fmtDateShort(row.original.created_at)}</span>
    ),
  }),
  // Pinned, not merely last: the list is wider than most windows, so a column
  // that scrolled would start life off the right edge, out of reach until the
  // operator scrolled sideways to find it.
  column.display({
    id: "actions",
    header: "Actions",
    // Hiding it would put peek and every write out of reach of the whole list.
    enableHiding: false,
    meta: {
      headClassName: cn(PINNED_RIGHT, "bg-muted"),
      // Inherited, so the pinned cell repaints with the row rather than reading
      // as a flat stripe over the peeked row's own colour.
      cellClassName: cn(PINNED_RIGHT, "bg-inherit"),
    },
    cell: ({ row }) => (
      <div className="flex items-center gap-1">
        <PeekTrigger org={row.original} />
        <OrganizationActions org={row.original} layout="menu" />
      </div>
    ),
  }),
]);
