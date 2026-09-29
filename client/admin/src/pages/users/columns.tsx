import { createColumnHelper } from "@tanstack/react-table";
import { Badge } from "@/components/ui/badge";
import type { DataTableFeatures } from "@/components/data-table";
import type { AdminUser } from "@/lib/gramAdminApi";
import { fmtDateShort } from "@/lib/utils";
import { UserActions } from "./UserActions";
const column = createColumnHelper<DataTableFeatures, AdminUser>();
export const USER_COLUMNS = column.columns([
  column.accessor("display_name", {
    header: "Name",
    cell: ({ row }) => row.original.display_name || "—",
  }),
  column.accessor("email", { header: "Email" }),
  column.accessor("organizations", {
    header: "Organizations",
    cell: ({ row }) => {
      const user = row.original;
      const first = user.organizations[0];
      return first ? (
        <div className="flex items-center gap-2">
          <Badge variant="secondary" title={`${first.name} (${first.slug})`}>
            {first.name} ({first.slug})
          </Badge>
          {user.organization_count > 1 && (
            <span className="text-muted-foreground text-sm">
              +{user.organization_count - 1} more
            </span>
          )}
        </div>
      ) : (
        <span className="text-muted-foreground">No organizations</span>
      );
    },
  }),
  column.accessor("last_login", {
    header: "Last login",
    cell: ({ row }) =>
      row.original.last_login ? (
        <time
          dateTime={row.original.last_login}
          title={new Date(row.original.last_login).toISOString()}
        >
          {fmtDateShort(row.original.last_login)}
        </time>
      ) : (
        <span className="text-muted-foreground">Not recorded</span>
      ),
  }),
  column.display({
    id: "actions",
    header: "Actions",
    enableHiding: false,
    meta: {
      headClassName: "sticky right-0 z-1 w-px bg-muted",
      cellClassName: "sticky right-0 z-1 w-px bg-inherit",
    },
    cell: ({ row }) => <UserActions user={row.original} />,
  }),
]);
