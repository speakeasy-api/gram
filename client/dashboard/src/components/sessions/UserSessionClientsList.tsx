import { format } from "date-fns";
import { useDeferredValue, useMemo, useState } from "react";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";

import { type FilterValue, useFilterState } from "@/components/filters";
import { Toolbar } from "@/components/ui/Toolbar";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@/components/ui/ContextMenu";
import { MoreActions } from "@/components/ui/MoreActions";
import { Column, SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
import { TablePagination } from "@/components/ui/TablePagination";
import { usePagedRows } from "@/components/ui/TablePagination/usePagedRows";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import {
  clientDocumentOrigin,
  userSessionClientSource,
} from "@/lib/user-session-client-source";
import { ClientSourceBadge } from "./ClientSourceBadge";
import { ListStateBoundary } from "./ListStateBoundary";
import { RevokeClientDialog } from "./RevokeClientDialog";
import {
  AGE_OPTIONS,
  CLIENT_FILTERS,
  CLIENT_SOURCE_OPTIONS,
  withinRecentWindow,
} from "./session-filters";

const PAGE_SIZE = 10;

/**
 * The clients registered against an issuer, DCR-registered and CIMD-resolved
 * alike, searchable by name and filterable by source and registration window.
 */
export function UserSessionClientsList({
  clients,
  isPending,
  isError,
  onRetry,
  filteredClientId,
  onViewSessions,
  onClientRevoked,
}: {
  clients: UserSessionClient[];
  isPending: boolean;
  isError: boolean;
  onRetry: () => void;
  /** When set, the table shows only this client. */
  filteredClientId?: string;
  /**
   * Drill-down target for a client's sessions. Optional: the MCP server detail
   * tab groups connections by client directly, so it has nothing to drill to
   * and omits the affordance rather than offering a dead one.
   */
  onViewSessions?: (client: UserSessionClient) => void;
  /**
   * Called after a revoke lands, so the owner can refresh the sessions the
   * revoke cascaded to. Revoking a client soft-deletes every session it
   * issued, which this table cannot refetch on its own.
   */
  onClientRevoked: (client: UserSessionClient) => void;
}): JSX.Element {
  const [sort, setSort] = useState<SortDescriptor | null>(null);
  const [search, setSearch] = useState("");
  // The target outlives the open flag so the dialog stays mounted through its
  // close animation instead of being torn out from under it.
  const [revokeTarget, setRevokeTarget] = useState<UserSessionClient | null>(
    null,
  );
  const [revokeOpen, setRevokeOpen] = useState(false);
  const { values, setValue, clearValue, clearAll } =
    useFilterState(CLIENT_FILTERS);
  const { hasScope } = useRBAC();
  const project = useProject();
  // Revoke is a write mutation the backend gates on project:write for THIS
  // project. hasScope without a resource id is existential across every
  // project the user holds grants in (ListGrants resolves principals per
  // organization), so an unscoped check would show Revoke to someone who is
  // read-only here but a writer elsewhere, and hand them a 403. Mirrors
  // pages/org/UserSessions.tsx, which scopes the same check by project id.
  const canRevoke = hasScope("project:write", project.id);

  const openRevoke = (client: UserSessionClient) => {
    setRevokeTarget(client);
    setRevokeOpen(true);
  };

  const deferredSearch = useDeferredValue(search);
  const visibleClients = useMemo(() => {
    const query = deferredSearch.trim().toLowerCase();
    // One timestamp for the whole pass, so two rows can't land on opposite
    // sides of a window boundary within a single filter run.
    const now = Date.now();
    return clients.filter((client) => {
      if (filteredClientId) return client.id === filteredClientId;
      if (query && !client.clientName.toLowerCase().includes(query)) {
        return false;
      }
      if (
        values.clientSource &&
        userSessionClientSource(client) !== values.clientSource
      ) {
        return false;
      }
      return withinRecentWindow(
        client.clientIdIssuedAt,
        values.clientRegistered,
        now,
      );
    });
  }, [clients, deferredSearch, filteredClientId, values]);

  const columns: Column<UserSessionClient>[] = [
    {
      /* client_name is chosen by the client and verified by nobody, so a CIMD
         row is labelled underneath with its document origin, which is the part
         of its identity it cannot forge. A DCR row has no such origin, so it
         shows the client_id Gram minted for it instead -- useful for
         correlating against logs, and human-sized (a CIMD client_id is the
         document URL, up to ~2KB). */
      key: "client",
      header: "Client",
      sortable: true,
      sortValue: (client) => client.clientName.toLowerCase(),
      width: "2fr",
      render: (client) => {
        const secondaryLabel = clientDocumentOrigin(client) ?? client.clientId;
        return (
          <div className="min-w-0">
            <Text
              variant="subheading"
              as="div"
              className="truncate text-sm"
              title={client.clientName}
            >
              {client.clientName}
            </Text>
            <Text small muted className="truncate" title={secondaryLabel}>
              {secondaryLabel}
            </Text>
          </div>
        );
      },
    },
    {
      key: "source",
      header: "Source",
      sortable: true,
      sortValue: (client) => userSessionClientSource(client),
      width: "0.6fr",
      render: (client) => <ClientSourceBadge client={client} />,
    },
    {
      key: "activeSessions",
      header: "Active sessions",
      sortable: true,
      sortValue: (client) => client.activeSessionCount,
      width: "0.9fr",
      render: (client) => (
        <ActiveSessionCountCell
          client={client}
          onViewSessions={onViewSessions}
        />
      ),
    },
    {
      key: "registered",
      header: "Registered",
      sortable: true,
      sortValue: (client) => client.clientIdIssuedAt.getTime(),
      width: "0.9fr",
      render: (client) => (
        <Text small muted>
          {format(client.clientIdIssuedAt, "PP")}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (client) => (
        <div
          className="flex w-full justify-end"
          onClick={(e) => e.stopPropagation()}
        >
          <MoreActions
            actions={[
              ...(onViewSessions
                ? [
                    {
                      label: "View sessions",
                      onClick: () => onViewSessions(client),
                    },
                  ]
                : []),
              ...(canRevoke
                ? [
                    {
                      label: "Revoke",
                      icon: "trash" as const,
                      destructive: true,
                      onClick: () => openRevoke(client),
                    },
                  ]
                : []),
            ]}
          />
        </div>
      ),
    },
  ];

  const sortedClients = sortTableData(
    visibleClients,
    columns,
    sort,
  ) as UserSessionClient[];
  const {
    page,
    pageRows: pageClients,
    setPage,
  } = usePagedRows({
    rows: sortedClients,
    pageSize: PAGE_SIZE,
    resetOn: [
      deferredSearch,
      filteredClientId,
      values.clientSource,
      values.clientRegistered,
    ],
  });

  return (
    <>
      {/* The drill-down already narrows this table to one row, so its own
          search and filters would only be able to hide that row. */}
      {!filteredClientId && (
        <Toolbar>
          <Toolbar.Search
            value={search}
            onChange={setSearch}
            debounceMs={150}
            placeholder="Search clients"
          />
          <Toolbar.Filters
            schema={CLIENT_FILTERS}
            values={values}
            optionsById={{
              clientSource: CLIENT_SOURCE_OPTIONS,
              clientRegistered: AGE_OPTIONS,
            }}
            onChange={setValue as (id: string, value: FilterValue) => void}
            onClear={clearValue as (id: string) => void}
            onClearAll={clearAll}
          />
          <Toolbar.Count>
            {sortedClients.length} client
            {sortedClients.length === 1 ? "" : "s"}
          </Toolbar.Count>
        </Toolbar>
      )}

      <ListStateBoundary
        isPending={isPending}
        isError={isError}
        isEmpty={clients.length === 0}
        errorMessage="Couldn't load clients."
        emptyMessage="No clients have registered with this server yet"
        onRetry={onRetry}
      >
        <section className="bg-card flex flex-col">
          <Table
            columns={columns}
            data={pageClients}
            rowKey={(client) => client.id}
            sort={sort}
            onSortChange={(next) => {
              setSort(next);
              setPage(0);
            }}
            noResultsMessage={
              <Text small muted className="p-3">
                No clients match these filters
              </Text>
            }
            renderRow={(client, rowElement) => (
              <ContextMenu>
                <ContextMenuTrigger asChild>{rowElement}</ContextMenuTrigger>
                <ContextMenuContent>
                  {onViewSessions && (
                    <ContextMenuItem onSelect={() => onViewSessions(client)}>
                      View sessions
                    </ContextMenuItem>
                  )}
                  {canRevoke && (
                    <ContextMenuItem
                      variant="destructive"
                      onSelect={() => openRevoke(client)}
                    >
                      Revoke client
                    </ContextMenuItem>
                  )}
                </ContextMenuContent>
              </ContextMenu>
            )}
          />
          <TablePagination
            page={page}
            pageSize={PAGE_SIZE}
            totalItems={sortedClients.length}
            onPageChange={setPage}
          />
        </section>
      </ListStateBoundary>

      {/* One dialog for the table rather than a closed one mounted per row. */}
      {revokeTarget && (
        <RevokeClientDialog
          client={revokeTarget}
          open={revokeOpen}
          onOpenChange={setRevokeOpen}
          onRevoked={() => {
            setRevokeOpen(false);
            onClientRevoked(revokeTarget);
          }}
        />
      )}
    </>
  );
}

/**
 * How many live sessions the client holds, doubling as the drill-down into
 * them — the same thing the row's "View sessions" action does. A client with
 * no sessions has nothing to drill into, so its zero stays inert.
 */
function ActiveSessionCountCell({
  client,
  onViewSessions,
}: {
  client: UserSessionClient;
  onViewSessions?: (client: UserSessionClient) => void;
}): JSX.Element {
  // Without a drill-down target the count is still worth reading, just inert.
  if (client.activeSessionCount === 0 || !onViewSessions) {
    return (
      <Text small muted>
        0
      </Text>
    );
  }

  return (
    <button
      type="button"
      className="text-foreground decoration-muted-foreground hover:decoration-foreground text-sm underline underline-offset-2"
      aria-label={`View ${client.activeSessionCount} active session${
        client.activeSessionCount === 1 ? "" : "s"
      } for ${client.clientName}`}
      onClick={(e) => {
        e.stopPropagation();
        onViewSessions(client);
      }}
    >
      {client.activeSessionCount}
    </button>
  );
}
