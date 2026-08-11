import { useState, useEffect, useCallback, useMemo, type JSX } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { SearchIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { badgeTone } from "@/lib/badgeTone";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { DataTable as Table, type Column } from "@/components/data-table";
import { cn } from "@/lib/utils";
import { listOrganizations, type AdminOrganization } from "@/lib/gramAdminApi";
import { ACCOUNT_TYPE_OPTIONS } from "@/lib/accountTypes";

function fmtDateShort(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleDateString();
}

// The columns and the empty list stay at module scope so the memoized rows
// below keep their identity while the operator types in the search box.
const ORG_COLUMNS: Column<AdminOrganization>[] = [
  {
    key: "name",
    header: "Name",
    render: (org) => <span className="text-sm">{org.name}</span>,
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

const NO_ORGS: AdminOrganization[] = [];

export function OrganizationsList(): JSX.Element {
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [accountType, setAccountType] = useState("");
  const [includeDisabled, setIncludeDisabled] = useState(false);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [cursorStack, setCursorStack] = useState<string[]>([]);
  const navigate = useNavigate();
  const qc = useQueryClient();

  // The functional updater lets React skip the render when paging is already
  // reset, which is the usual case.
  const resetPaging = useCallback(() => {
    setCursor(undefined);
    setCursorStack((s) => (s.length === 0 ? s : []));
  }, []);

  // A search term that settles back on the current one leaves paging alone, so
  // a typo and a backspace do not throw the operator back to the first page.
  useEffect(() => {
    const next = search.trim();
    if (next === debouncedSearch) return;
    const t = setTimeout(() => {
      setDebouncedSearch(next);
      resetPaging();
    }, 300);
    return () => clearTimeout(t);
  }, [search, debouncedSearch, resetPaging]);

  const queryKey = useMemo(
    () => [
      "gram-admin-organizations",
      { q: debouncedSearch, accountType, includeDisabled, cursor },
    ],
    [debouncedSearch, accountType, includeDisabled, cursor],
  );

  const { data, isLoading, isError, error, isPlaceholderData } = useQuery({
    queryKey,
    queryFn: () =>
      listOrganizations({
        q: debouncedSearch || undefined,
        account_type: accountType || undefined,
        include_disabled: includeDisabled || undefined,
        cursor,
        limit: 50,
      }),
    // Every filter and every page is a separate cache entry. Without this the
    // table empties on each change and the rows jump.
    placeholderData: keepPreviousData,
  });

  const goNext = () => {
    if (!data?.next_cursor) return;
    setCursorStack((s) => [...s, cursor ?? ""]);
    setCursor(data.next_cursor);
  };

  const goPrev = () => {
    if (cursorStack.length === 0) return;
    // An empty string on the stack is the first page, which has no cursor.
    const prev = cursorStack[cursorStack.length - 1];
    setCursor(prev || undefined);
    setCursorStack(cursorStack.slice(0, -1));
  };

  const handleRowClick = useCallback(
    (org: AdminOrganization) => {
      const idOrSlug = org.slug || org.id;
      // The row already holds the whole record, so the detail page paints and
      // starts its own queries without a round trip to organization.get.
      qc.setQueryData(["gram-admin-organization", idOrSlug], org);
      void navigate({ to: "/organizations/$idOrSlug", params: { idOrSlug } });
    },
    [navigate, qc],
  );

  const orgs = data?.organizations ?? NO_ORGS;

  const rows = useMemo(
    () =>
      orgs.map((org) => (
        <Table.Row
          key={org.id}
          row={org}
          columns={ORG_COLUMNS}
          onClick={handleRowClick}
        />
      )),
    [orgs, handleRowClick],
  );

  return (
    <div className="space-y-6">
      <section>
        <div className="mb-2 flex items-center gap-2">
          <div className="relative w-80">
            <SearchIcon className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-4 -translate-y-1/2" />
            <Input
              placeholder="Search by name or slug..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full py-1.5 pr-2 pl-8"
            />
          </div>
          <Select
            value={accountType || "all"}
            onValueChange={(v) => {
              setAccountType(v === "all" ? "" : v);
              resetPaging();
            }}
          >
            <SelectTrigger className="h-auto w-auto px-2 py-1.5">
              <SelectValue placeholder="All types" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All types</SelectItem>
              {ACCOUNT_TYPE_OPTIONS.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant={includeDisabled ? "default" : "ghost"}
            size="xs"
            onClick={() => {
              setIncludeDisabled((v) => !v);
              resetPaging();
            }}
          >
            {includeDisabled ? "Hide disabled" : "Show disabled"}
          </Button>
        </div>

        {/* A failed refetch keeps the previous rows, so the failure has to show
            outside the empty state or the operator reads stale data as fresh. */}
        {isError && (
          <div className="text-muted-foreground mb-2 text-sm">
            Could not refresh organizations:{" "}
            {(error as Error)?.message ?? "unknown error"}
          </div>
        )}

        <div
          className={cn(
            "max-h-[60vh] overflow-auto rounded-lg border",
            isPlaceholderData && "opacity-60",
          )}
        >
          <Table columns={ORG_COLUMNS} cellPadding="condensed">
            <Table.Header columns={ORG_COLUMNS} />
            <Table.Body>
              {orgs.length === 0 ? (
                <Table.NoResultsMessage>
                  <span className="text-muted-foreground text-sm">
                    {isLoading ? "Loading..." : "No organizations found"}
                  </span>
                </Table.NoResultsMessage>
              ) : (
                rows
              )}
            </Table.Body>
          </Table>
        </div>

        {/* Placeholder rows belong to the previous filter, and so does the
            cursor beside them. Both controls wait for the real page. */}
        <div className="mt-3 flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            size="xs"
            disabled={isPlaceholderData || cursorStack.length === 0}
            onClick={goPrev}
          >
            Previous
          </Button>
          <Button
            variant="ghost"
            size="xs"
            disabled={isPlaceholderData || !data?.next_cursor}
            onClick={goNext}
          >
            Next
          </Button>
        </div>
      </section>
    </div>
  );
}
