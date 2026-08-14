import { Link } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";

import type { DataTableFeatures } from "@/components/data-table";
import { Trial } from "@/components/Trial";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { cn, fmtDateShort } from "@/lib/utils";

import { OrganizationActions } from "./OrganizationActions";
import { PeekTrigger } from "./PeekTrigger";

const column = createColumnHelper<DataTableFeatures, AdminOrganization>();

// Module scope gives the array one identity for the life of the tab, so the
// table does not rebuild its column model on every render. The header of each
// column doubles as its label in the Columns control.
export const ORG_COLUMNS = column.columns([
  // First, not last: peek hides five columns while it is open, so a trailing
  // control would slide sideways at the moment the operator is using it.
  column.display({
    id: "peek",
    // A plain string, so the Columns control lists it as "Peek" rather than
    // falling back to the column id.
    header: "Peek",
    // Hiding the control would put peek back out of reach of the keyboard.
    enableHiding: false,
    cell: ({ row }) => <PeekTrigger org={row.original} />,
  }),
  // Beside peek rather than trailing the row, for the reason above: the row
  // menu is the control an operator reaches for while peek is open, and it is
  // the five hidden columns' width away from where it was if it sits last.
  //
  // Deliberately in neither PEEK_HIDDEN_COLUMNS nor PEEK_COLUMN_OVERRIDES: an
  // open peek is no reason to take the other rows' actions away, and hiding a
  // control the Columns menu cannot bring back would strand it.
  column.display({
    id: "actions",
    header: "Actions",
    // Hiding the menu would put disable, re-enable and extend out of reach for
    // the whole list, and the peek panel's copy of them covers one record.
    enableHiding: false,
    cell: ({ row }) => <OrganizationActions org={row.original} layout="menu" />,
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
]);
