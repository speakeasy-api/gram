import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useTable } from "@tanstack/react-table";
import { useCallback, useRef, useState, type JSX } from "react";
import { dataTableFeatures, DataTable as Table } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { useOnUnmount } from "@/hooks/useOnUnmount";
import { usersListQuery } from "@/lib/adminQueries";
import {
  errorMessage,
  type AdminListUsersResult,
  type AdminUser,
} from "@/lib/gramAdminApi";
import { usersSearchSchema, type UsersSearch } from "@/lib/usersSearchRoute";
import { parseUserSearch } from "@/lib/userSearch";
import { UserSearchInput } from "./UserSearchInput";
import { USER_COLUMNS } from "./columns";

declare module "@tanstack/react-router" {
  interface HistoryState {
    usersEdit?: string;
  }
}
const EMPTY: AdminUser[] = [];
// A string-only editor cannot see same-string navigation. Consume our own
// replace marker once; every other navigation identity remounts its history.
export function UsersList(): JSX.Element {
  const location = useLocation();
  const ownEdit = useRef<string | undefined>(undefined);
  const markEdit = useCallback((token: string) => {
    ownEdit.current = token;
  }, []);
  const [identity, setIdentity] = useState({
    key: location.state.__TSR_key,
    epoch: 0,
  });
  if (identity.key !== location.state.__TSR_key) {
    const own =
      ownEdit.current !== undefined &&
      ownEdit.current === location.state.usersEdit;
    ownEdit.current = undefined;
    setIdentity({
      key: location.state.__TSR_key,
      epoch: identity.epoch + (own ? 0 : 1),
    });
  }
  // Read search and navigation identity from the same location snapshot. During
  // Back/Forward, route-match useSearch can briefly still describe the old URL.
  return (
    <UsersPage
      key={identity.epoch}
      markEdit={markEdit}
      search={usersSearchSchema(location.search)}
    />
  );
}
function UsersPage({
  markEdit,
  search,
}: {
  markEdit: (token: string) => void;
  search: UsersSearch;
}) {
  const navigate = useNavigate({ from: "/users/" });
  const q = search.q ?? "";
  const page = search.page ?? 1;
  const [draft, setDraft] = useState(q);
  const parsed = parseUserSearch(draft);
  const valid = parseUserSearch(q).ok;
  const pending = draft !== q;
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useOnUnmount(() => clearTimeout(timer.current));
  const commit = (value: string): void => {
    const token = crypto.randomUUID();
    markEdit(token);
    void navigate({
      search: { q: value || undefined, page: 1 },
      replace: true,
      state: (prev) => ({ ...prev, usersEdit: token }),
    });
  };
  const changeDraft = (value: string): void => {
    setDraft(value);
    clearTimeout(timer.current);
    if (value !== q && parseUserSearch(value).ok) {
      timer.current = setTimeout(() => commit(value), 300);
    }
  };
  const query = useQuery({
    ...usersListQuery({ q, page, limit: 50 }),
    enabled: valid && parsed.ok && !pending,
    placeholderData: keepPreviousData,
  });
  const [lastValid, setLastValid] = useState<{
    q: string;
    page: number;
    data: AdminListUsersResult;
  }>();
  if (
    query.data &&
    !query.isPlaceholderData &&
    (lastValid?.data !== query.data ||
      lastValid.q !== q ||
      lastValid.page !== page)
  )
    setLastValid({ q, page, data: query.data });
  const stale = !parsed.ok || pending || query.isPlaceholderData;
  const shown = stale ? lastValid?.data : query.data;
  const table = useTable({
    features: dataTableFeatures,
    data: shown?.users ?? EMPTY,
    columns: USER_COLUMNS,
    getRowId: (user) => user.id,
  });
  const blocked =
    stale || !valid || query.isFetching || query.isError || !shown;
  return (
    <div className="flex h-full flex-col">
      <section className="flex min-h-0 flex-1 flex-col">
        <div className="mb-4 flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <UserSearchInput
              value={draft}
              onChange={changeDraft}
              error={parsed.ok ? undefined : parsed.message}
            />
          </div>
          <Button
            variant="ghost"
            onClick={() => {
              clearTimeout(timer.current);
              setDraft("");
              commit("");
            }}
          >
            Clear search
          </Button>
        </div>
        {stale && lastValid && (
          <p role="status" className="text-muted-foreground mb-2 text-sm">
            Showing last valid results for {lastValid.q || "all users"} (page{" "}
            {lastValid.page}).
          </p>
        )}
        {query.isError && (
          <p role="alert">
            Could not refresh users: {errorMessage(query.error)}{" "}
            <Button
              variant="ghost"
              onClick={() => void query.refetch()}
              disabled={!valid || !parsed.ok || pending}
            >
              Retry
            </Button>
          </p>
        )}
        <div className="flex min-h-0 flex-1 flex-col rounded-lg border">
          <div
            role="region"
            aria-label="Users table"
            className="min-h-0 flex-1 overflow-auto"
          >
            <Table>
              <Table.Header table={table} />
              <Table.Body>
                {table.getRowModel().rows.length ? (
                  table
                    .getRowModel()
                    .rows.map((row) => (
                      <Table.Row
                        key={row.id}
                        row={row}
                        className="bg-background hover:bg-muted has-aria-expanded:bg-muted"
                      />
                    ))
                ) : (
                  <Table.NoResultsMessage colSpan={USER_COLUMNS.length}>
                    {!parsed.ok
                      ? "Enter a valid search to find users."
                      : query.isError
                        ? "Unable to load users"
                        : query.isPending
                          ? "Loading…"
                          : "No users found"}
                  </Table.NoResultsMessage>
                )}
              </Table.Body>
            </Table>
          </div>
        </div>
        <div className="mt-3 flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            size="xs"
            disabled={blocked || page === 1}
            onClick={() =>
              void navigate({ search: { q: search.q, page: page - 1 } })
            }
          >
            Previous
          </Button>
          <Button
            variant="ghost"
            size="xs"
            disabled={blocked || page * 50 >= (shown?.total ?? 0)}
            onClick={() =>
              void navigate({ search: { q: search.q, page: page + 1 } })
            }
          >
            Next
          </Button>
        </div>
      </section>
    </div>
  );
}
