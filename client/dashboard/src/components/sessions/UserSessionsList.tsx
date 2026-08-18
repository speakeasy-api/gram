import { format } from "date-fns";
import { useDeferredValue, useMemo, useState } from "react";
import type { UserSession } from "@gram/client/models/components/usersession.js";

import { type FilterValue, useFilterState } from "@/components/filters";
import { Toolbar } from "@/components/ui/Toolbar";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/ContextMenu";
import { Icon } from "@/components/ui/Icon";
import { MoreActions } from "@/components/ui/MoreActions";
import { Column, SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { TablePagination } from "@/components/ui/TablePagination";
import { usePagedRows } from "@/components/ui/TablePagination/usePagedRows";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { getInitials } from "@/lib/initials";
import {
  sessionStatus,
  sessionTimeLabel,
  subjectLabel,
} from "@/lib/user-session-status";
import { ClientSourceBadge } from "./ClientSourceBadge";
import { ListStateBoundary } from "./ListStateBoundary";
import { RevokeSessionDialog } from "./RevokeSessionDialog";
import {
  AGE_OPTIONS,
  EXPIRY_OPTIONS,
  SESSION_FILTERS,
  SESSION_FILTERS_IN_CLIENT,
  withinRecentWindow,
  withinUpcomingWindow,
} from "./session-filters";

const PAGE_SIZE = 10;

/**
 * An MCP server's active sessions, searchable by subject and filterable by
 * client and by created/expiry window. Every row is already in memory, so
 * search, filters, sort, and paging all run client-side.
 */
export function UserSessionsList({
  sessions,
  isPending,
  isError,
  onRetry,
  onRevoked,
  clientFilterId,
  clientOptions,
}: {
  sessions: UserSession[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  onRevoked: () => void;
  /** Set while the tab is drilled into one client's sessions. */
  clientFilterId?: string;
  /**
   * Choices for the Client filter, from the clients listing. Keyed by client
   * id rather than name: client_name is client-chosen, so two registrations
   * can pick the same one.
   */
  clientOptions: { value: string; label: string }[];
}): JSX.Element {
  const [sort, setSort] = useState<SortDescriptor | null>(null);
  const [search, setSearch] = useState("");
  // The target outlives the open flag so the dialog stays mounted through its
  // close animation instead of being torn out from under it.
  const [revokeTarget, setRevokeTarget] = useState<UserSession | null>(null);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const { values, setValue, clearValue, clearAll } =
    useFilterState(SESSION_FILTERS);
  const { hasScope } = useRBAC();
  const project = useProject();
  // Revoke is a write mutation the backend gates on project:write for THIS
  // project. hasScope without a resource id is existential across every
  // project the user holds grants in (ListGrants resolves principals per
  // organization), so an unscoped check would show Revoke to someone who is
  // read-only here but a writer elsewhere, and hand them a 403. Mirrors
  // pages/org/UserSessions.tsx, which scopes the same check by project id.
  const canRevokeInProject = hasScope("project:write", project.id);

  // The list is served pre-filtered to active sessions, but a row can cross
  // its refresh deadline while the page is open. Revoking one of those would
  // 404, so the affordance follows the row's own status.
  const canRevoke = (session: UserSession) =>
    canRevokeInProject && sessionStatus(session) === "active";

  const openRevoke = (session: UserSession) => {
    setRevokeTarget(session);
    setRevokeOpen(true);
  };

  const deferredSearch = useDeferredValue(search);
  const visibleSessions = useMemo(() => {
    const query = deferredSearch.trim().toLowerCase();
    // One timestamp for the whole pass, so two rows can't land on opposite
    // sides of a window boundary within a single filter run.
    const now = Date.now();
    return sessions.filter((session) => {
      if (clientFilterId && session.userSessionClientId !== clientFilterId) {
        return false;
      }
      if (query && !subjectLabel(session).toLowerCase().includes(query)) {
        return false;
      }
      // While drilled into one client, the Client chip is hidden, so a value
      // left in the URL from before must not narrow the rows further.
      if (
        !clientFilterId &&
        values.sessionClient &&
        session.userSessionClientId !== values.sessionClient
      ) {
        return false;
      }
      return (
        withinRecentWindow(session.createdAt, values.sessionCreated, now) &&
        withinUpcomingWindow(
          session.refreshExpiresAt,
          values.sessionExpires,
          now,
        )
      );
    });
  }, [sessions, deferredSearch, clientFilterId, values]);

  const columns: Column<UserSession>[] = [
    {
      key: "subject",
      header: "Subject",
      sortable: true,
      sortValue: (session) => subjectLabel(session).toLowerCase(),
      width: "1.8fr",
      render: (session) => <SubjectCell session={session} />,
    },
    {
      key: "client",
      header: "OAuth client",
      sortable: true,
      sortValue: (session) => (session.clientName ?? "").toLowerCase(),
      width: "1.3fr",
      render: (session) => (
        <div className="flex min-w-0 items-center gap-2">
          {/* min-w-0: a flex item defaults to min-width:auto, which stops
              `truncate` engaging. client_name is client-supplied and up to
              256 bytes, so without this it widens the whole table. */}
          <Text small muted className="min-w-0 truncate">
            {session.clientName ?? "—"}
          </Text>
          {session.clientName && <ClientSourceBadge client={session} />}
        </div>
      ),
    },
    {
      key: "created",
      header: "Created",
      sortable: true,
      sortValue: (session) => session.createdAt.getTime(),
      width: "0.8fr",
      render: (session) => (
        <Text small muted>
          {format(session.createdAt, "PP")}
        </Text>
      ),
    },
    {
      key: "expires",
      header: "Expires",
      sortable: true,
      // Sorted on the refresh deadline the label is derived from, so the
      // ordering matches what the column reads.
      sortValue: (session) => session.refreshExpiresAt.getTime(),
      width: "1fr",
      render: (session) => (
        <Text small muted className="min-w-0 truncate">
          {sessionTimeLabel(session)}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (session) =>
        canRevoke(session) ? (
          <div
            className="flex w-full justify-end"
            onClick={(e) => e.stopPropagation()}
          >
            <MoreActions
              actions={[
                {
                  label: "Revoke",
                  icon: "trash" as const,
                  destructive: true,
                  onClick: () => openRevoke(session),
                },
              ]}
            />
          </div>
        ) : null,
    },
  ];

  const sortedSessions = sortTableData(
    visibleSessions,
    columns,
    sort,
  ) as UserSession[];
  const {
    page,
    pageRows: pageSessions,
    setPage,
  } = usePagedRows({
    rows: sortedSessions,
    pageSize: PAGE_SIZE,
    resetOn: [
      deferredSearch,
      clientFilterId,
      values.sessionClient,
      values.sessionCreated,
      values.sessionExpires,
    ],
  });

  return (
    <>
      <Toolbar>
        <Toolbar.Search
          value={search}
          onChange={setSearch}
          debounceMs={150}
          placeholder="Search subjects"
        />
        <Toolbar.Filters
          schema={clientFilterId ? SESSION_FILTERS_IN_CLIENT : SESSION_FILTERS}
          values={values}
          optionsById={{
            sessionClient: clientOptions,
            sessionCreated: AGE_OPTIONS,
            sessionExpires: EXPIRY_OPTIONS,
          }}
          onChange={setValue as (id: string, value: FilterValue) => void}
          onClear={clearValue as (id: string) => void}
          onClearAll={clearAll}
        />
      </Toolbar>

      <ListStateBoundary
        isPending={isPending}
        isError={isError}
        isEmpty={sessions.length === 0}
        errorMessage="Couldn't load sessions."
        emptyMessage={
          clientFilterId
            ? "No active sessions for this client"
            : "No active sessions"
        }
        onRetry={onRetry}
      >
        <section className="bg-card flex flex-col">
          <Table
            columns={columns}
            data={pageSessions}
            rowKey={(session) => session.id}
            sort={sort}
            onSortChange={(next) => {
              setSort(next);
              setPage(0);
            }}
            noResultsMessage={
              <Text small muted className="p-3">
                {clientFilterId
                  ? "No active sessions for this client"
                  : "No sessions match these filters"}
              </Text>
            }
            renderRow={(session, rowElement) =>
              canRevoke(session) ? (
                <ContextMenu>
                  <ContextMenuTrigger asChild>{rowElement}</ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem
                      variant="destructive"
                      onSelect={() => openRevoke(session)}
                    >
                      Revoke session
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              ) : (
                rowElement
              )
            }
          />
          <TablePagination
            page={page}
            pageSize={PAGE_SIZE}
            totalItems={sortedSessions.length}
            onPageChange={setPage}
          />
        </section>
      </ListStateBoundary>

      {/* One dialog for the table rather than a closed one mounted per row. */}
      {revokeTarget && (
        <RevokeSessionDialog
          session={revokeTarget}
          open={revokeOpen}
          onOpenChange={setRevokeOpen}
          onRevoked={() => {
            setRevokeOpen(false);
            onRevoked();
          }}
        />
      )}
    </>
  );
}

/**
 * The session's subject, with the member's avatar when the subject resolves to
 * a Gram user. API key and anonymous subjects have no directory identity, so
 * they get a neutral glyph in the same slot to keep the column aligned.
 */
function SubjectCell({ session }: { session: UserSession }): JSX.Element {
  const label = subjectLabel(session);
  const isUser = session.subjectType === "user";

  return (
    <div className="flex min-w-0 items-center gap-3">
      <Avatar className="size-8 shrink-0">
        {isUser && session.subjectPhotoUrl && (
          <AvatarImage src={session.subjectPhotoUrl} alt="" />
        )}
        <AvatarFallback className="text-xs font-semibold">
          {isUser ? (
            getInitials(label)
          ) : (
            <Icon
              name={session.subjectType === "apikey" ? "key" : "circle-help"}
              className="text-muted-foreground size-4"
            />
          )}
        </AvatarFallback>
      </Avatar>
      <Text
        variant="subheading"
        as="div"
        className="min-w-0 truncate text-sm"
        title={label}
      >
        {label}
      </Text>
    </div>
  );
}
