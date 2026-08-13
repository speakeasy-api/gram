import { Link } from "@tanstack/react-router";

import type { Column } from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { cn } from "@/lib/utils";

function fmtDateShort(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleDateString();
}

// Module scope, so the memoized rows on the page keep their identity while the
// operator types in the search box. The header of each column doubles as its
// label in the Columns control.
export const ORG_COLUMNS: Column<AdminOrganization>[] = [
  {
    key: "name",
    header: "Name",
    // The link, not the row, carries the keyboard path and the accessible
    // name. It also lets the operator open the organization in a new tab.
    render: (org) => (
      <Link
        to="/organizations/$idOrSlug"
        params={{ idOrSlug: org.slug || org.id }}
        className="text-sm underline-offset-4 hover:underline focus-visible:underline"
      >
        {org.name}
      </Link>
    ),
  },
  {
    key: "slug",
    header: "Slug",
    render: (org) => <span className="text-sm">{org.slug}</span>,
  },
  {
    key: "account_type",
    header: "Type",
    render: (org) => (
      <Badge variant="outline" className={badgeTone.neutral}>
        {org.account_type}
      </Badge>
    ),
  },
  {
    key: "member_count",
    header: "Members",
    render: (org) => <span className="text-sm">{org.member_count}</span>,
  },
  {
    key: "workos_id",
    header: "WorkOS",
    render: (org) => (
      <span
        className={cn("text-sm", !org.workos_id && "text-muted-foreground")}
      >
        {org.workos_id ? `${org.workos_id.substring(0, 12)}...` : "-"}
      </span>
    ),
  },
  {
    key: "disabled_at",
    header: "Disabled",
    render: (org) => (
      <span
        className={cn("text-sm", !org.disabled_at && "text-muted-foreground")}
      >
        {org.disabled_at ? fmtDateShort(org.disabled_at) : "-"}
      </span>
    ),
  },
  {
    key: "free_trial_ends_at",
    header: "Trial ends",
    render: (org) => (
      <span
        className={cn(
          "text-sm",
          !org.free_trial_ends_at && "text-muted-foreground",
        )}
      >
        {fmtDateShort(org.free_trial_ends_at)}
      </span>
    ),
  },
  {
    key: "created_at",
    header: "Created",
    render: (org) => (
      <span className="text-sm">{fmtDateShort(org.created_at)}</span>
    ),
  },
];
