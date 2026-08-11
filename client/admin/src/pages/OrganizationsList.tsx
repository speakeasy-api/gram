import { useState, useEffect, useMemo, type JSX } from "react";
import { useNavigate } from "react-router";
import { useQuery } from "@tanstack/react-query";
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

function fmtDateShort(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleDateString();
}

const accountTypeOptions = ["free", "pro", "enterprise"];

export function OrganizationsList(): JSX.Element {
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [accountType, setAccountType] = useState("");
  const [includeDisabled, setIncludeDisabled] = useState(false);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [cursorStack, setCursorStack] = useState<string[]>([]);
  const navigate = useNavigate();

  useEffect(() => {
    const t = setTimeout(() => {
      setDebouncedSearch(search.trim());
      setCursor(undefined);
      setCursorStack([]);
    }, 300);
    return () => clearTimeout(t);
  }, [search]);

  // Reset paging when filters change.
  useEffect(() => {
    setCursor(undefined);
    setCursorStack([]);
  }, [accountType, includeDisabled]);

  const queryKey = useMemo(
    () => [
      "gram-admin-organizations",
      { q: debouncedSearch, accountType, includeDisabled, cursor },
    ],
    [debouncedSearch, accountType, includeDisabled, cursor],
  );

  const { data, isLoading, isError, error } = useQuery({
    queryKey,
    queryFn: () =>
      listOrganizations({
        q: debouncedSearch || undefined,
        account_type: accountType || undefined,
        include_disabled: includeDisabled || undefined,
        cursor,
        limit: 50,
      }),
  });

  const goNext = () => {
    if (!data?.next_cursor) return;
    setCursorStack((s) => [...s, cursor ?? ""]);
    setCursor(data.next_cursor);
  };

  const goPrev = () => {
    setCursorStack((s) => {
      if (s.length === 0) return s;
      const prev = s[s.length - 1];
      setCursor(prev || undefined);
      return s.slice(0, -1);
    });
  };

  const columns: Column<AdminOrganization>[] = [
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

  const orgs = data?.organizations ?? [];

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
            onValueChange={(v) => setAccountType(v === "all" ? "" : v)}
          >
            <SelectTrigger className="h-auto w-auto px-2 py-1.5">
              <SelectValue placeholder="All types" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All types</SelectItem>
              {accountTypeOptions.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant={includeDisabled ? "default" : "ghost"}
            size="xs"
            onClick={() => setIncludeDisabled((v) => !v)}
          >
            {includeDisabled ? "Hiding none" : "Hide disabled"}
          </Button>
        </div>

        <div className="max-h-[60vh] overflow-auto rounded-lg border">
          <Table columns={columns} cellPadding="condensed">
            <Table.Header columns={columns} />
            <Table.Body>
              {isLoading || orgs.length === 0 ? (
                <Table.NoResultsMessage>
                  <span className="text-muted-foreground text-sm">
                    {isLoading
                      ? "Loading..."
                      : isError
                        ? `Error: ${(error as Error)?.message ?? "unknown"}`
                        : "No organizations found"}
                  </span>
                </Table.NoResultsMessage>
              ) : (
                orgs.map((org) => (
                  <Table.Row
                    key={org.id}
                    row={org}
                    columns={columns}
                    onClick={() => {
                      void navigate(
                        `/organizations/${encodeURIComponent(org.slug || org.id)}`,
                      );
                    }}
                  />
                ))
              )}
            </Table.Body>
          </Table>
        </div>

        <div className="mt-3 flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            size="xs"
            disabled={cursorStack.length === 0}
            onClick={goPrev}
          >
            Previous
          </Button>
          <Button
            variant="ghost"
            size="xs"
            disabled={!data?.next_cursor}
            onClick={goNext}
          >
            Next
          </Button>
        </div>
      </section>
    </div>
  );
}
