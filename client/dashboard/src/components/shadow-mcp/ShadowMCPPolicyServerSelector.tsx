import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import {
  Badge,
  Button,
  type Column,
  Dialog,
  Icon,
  type SortDescriptor,
  Table,
  sortTableData,
} from "@speakeasy-api/moonshine";
import { useCallback, useDeferredValue, useMemo, useState } from "react";
import {
  ShadowMCPInventoryServerCell,
  ShadowMCPInventoryUsageCell,
} from "./ShadowMCPInventoryCells";
import {
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusLabel,
  type ShadowMCPInventoryStatus,
} from "./shadowMCPInventoryStatus";

export type ShadowMCPPolicyServerSelectorProps = {
  servers: ShadowMCPInventoryServer[];
  originalURLs: ReadonlySet<string>;
  selectedURLs: ReadonlySet<string>;
  onSelectionChange: (next: Set<string>) => void;
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
};

type PolicyServerAction = "add" | "remove" | "no-change";

type PolicyServerChange = {
  action: PolicyServerAction;
  server: ShadowMCPInventoryServer;
};

const POLICY_SERVER_ACTION_SORT_VALUE: Record<PolicyServerAction, number> = {
  remove: 0,
  add: 1,
  "no-change": 2,
};

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

function selectedCountLabel(count: number): string {
  return countLabel(count, "server selected", "servers selected");
}

function selectorEmptyMessage(search: string): string {
  if (search.trim()) return "No matching servers";
  return "No Shadow MCP servers";
}

function inventoryAccessStatus(
  server: ShadowMCPInventoryServer,
): ShadowMCPInventoryStatus {
  switch (server.access) {
    case "allowed":
      return "allowed";
    case "blocked":
      return "blocked";
    case "none":
      return "observed";
  }
}

function serverLabel(server: ShadowMCPInventoryServer): string {
  return server.serverName || server.urlHost;
}

function comparePolicyServerNames(
  left: PolicyServerChange,
  right: PolicyServerChange,
): number {
  return serverLabel(left.server).localeCompare(
    serverLabel(right.server),
    undefined,
    { numeric: true, sensitivity: "base" },
  );
}

function policyServerAction(
  url: string,
  originalURLs: ReadonlySet<string>,
  selectedURLs: ReadonlySet<string>,
): PolicyServerAction {
  if (!originalURLs.has(url)) return "add";
  if (!selectedURLs.has(url)) return "remove";
  return "no-change";
}

function StatusCell({ server }: { server: ShadowMCPInventoryServer }) {
  const status = inventoryAccessStatus(server);

  return (
    <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
      <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
    </Badge>
  );
}

function EmptyServerSelection({ onSelect }: { onSelect: () => void }) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-10 text-center">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon
          aria-hidden="true"
          name="shield-check"
          className="text-muted-foreground h-6 w-6"
        />
      </div>
      <Type variant="subheading" className="mb-1">
        No servers allowed yet
      </Type>
      <Type small muted className="mb-4 max-w-md">
        Select any Shadow MCP servers that should remain available when this
        policy blocks access.
      </Type>
      <Button type="button" variant="primary" onClick={onSelect}>
        Select servers
      </Button>
    </div>
  );
}

function PolicyServerActionBadge({ action }: { action: PolicyServerAction }) {
  switch (action) {
    case "add":
      return <Badge variant="success">Add</Badge>;
    case "remove":
      return <Badge variant="destructive">Remove</Badge>;
    case "no-change":
      return <Badge variant="neutral">No change</Badge>;
  }
}

const APPLIED_SERVER_COLUMNS: Column<PolicyServerChange>[] = [
  {
    key: "action",
    header: "Action",
    sortable: true,
    sortValue: ({ action }) => POLICY_SERVER_ACTION_SORT_VALUE[action],
    width: "112px",
    render: (row) => <PolicyServerActionBadge action={row.action} />,
  },
  {
    key: "server",
    header: "Server",
    sortable: true,
    sortValue: ({ server }) => serverLabel(server).trim().toLowerCase(),
    width: "0.35fr",
    render: ({ server }) => {
      const label = serverLabel(server);
      return (
        <Type variant="small" className="truncate font-medium" title={label}>
          {label}
        </Type>
      );
    },
  },
  {
    key: "url",
    header: "URL",
    sortable: true,
    sortValue: ({ server }) => server.canonicalServerUrl.trim().toLowerCase(),
    width: "1fr",
    render: ({ server }) => (
      <Type
        muted
        small
        className="truncate font-mono text-xs"
        title={server.canonicalServerUrl}
      >
        {server.canonicalServerUrl}
      </Type>
    ),
  },
];

function AppliedServerTable({ rows }: { rows: PolicyServerChange[] }) {
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "action",
    direction: "asc",
  });

  const setFocusableTableBody = useCallback(
    (element: HTMLTableSectionElement | null) => {
      if (element) element.tabIndex = 0;
    },
    [],
  );

  const sortedRows = useMemo(() => {
    const rowsByServer = rows.toSorted(comparePolicyServerNames);
    return sortTableData(
      rowsByServer,
      APPLIED_SERVER_COLUMNS,
      sort,
    ) as PolicyServerChange[];
  }, [rows, sort]);

  return (
    <Table
      columns={APPLIED_SERVER_COLUMNS}
      cellPadding="condensed"
      className="grid-rows-[auto_minmax(0,1fr)]"
    >
      <Table.Header
        columns={APPLIED_SERVER_COLUMNS}
        sort={sort}
        onSortChange={setSort}
      />
      <Table.Body
        columns={APPLIED_SERVER_COLUMNS}
        data={sortedRows}
        rowKey={({ server }) => server.canonicalServerUrl}
        ref={setFocusableTableBody}
        className="focus-visible:ring-ring max-h-[200px] content-start overflow-y-auto focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
      />
    </Table>
  );
}

export function ShadowMCPPolicyServerSelector({
  servers,
  originalURLs,
  selectedURLs,
  onSelectionChange,
  isLoading,
  error,
  onRetry,
}: ShadowMCPPolicyServerSelectorProps): JSX.Element {
  const [open, setOpen] = useState(false);
  const [draftURLs, setDraftURLs] = useState<Set<string>>(
    () => new Set(selectedURLs),
  );
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);
  const [sort, setSort] = useState<SortDescriptor | null>({
    id: "lastCalled",
    direction: "desc",
  });

  const appliedRows = useMemo(
    () =>
      servers
        .filter((server) => {
          const url = server.canonicalServerUrl;
          return originalURLs.has(url) || selectedURLs.has(url);
        })
        .map((server) => ({
          action: policyServerAction(
            server.canonicalServerUrl,
            originalURLs,
            selectedURLs,
          ),
          server,
        })),
    [originalURLs, selectedURLs, servers],
  );

  const filteredServers = useMemo(() => {
    const normalizedSearch = deferredSearch.trim().toLowerCase();
    if (normalizedSearch.length === 0) return servers;

    return servers.filter((server) => {
      const nameMatches = server.serverName
        ?.toLowerCase()
        .includes(normalizedSearch);
      return (
        nameMatches === true ||
        server.canonicalServerUrl.toLowerCase().includes(normalizedSearch)
      );
    });
  }, [deferredSearch, servers]);

  const toggleURL = useCallback((url: string) => {
    setDraftURLs((currentURLs) => {
      const nextURLs = new Set(currentURLs);
      if (nextURLs.has(url)) {
        nextURLs.delete(url);
      } else {
        nextURLs.add(url);
      }
      return nextURLs;
    });
  }, []);

  const columns = useMemo<Column<ShadowMCPInventoryServer>[]>(
    () => [
      {
        key: "selected",
        header: <span className="sr-only">Selected</span>,
        width: "44px",
        render: (server) => (
          <div onClick={(event) => event.stopPropagation()}>
            <Checkbox
              aria-label={`Select ${serverLabel(server)}`}
              checked={draftURLs.has(server.canonicalServerUrl)}
              onCheckedChange={() => toggleURL(server.canonicalServerUrl)}
            />
          </div>
        ),
      },
      {
        key: "server",
        header: "Server",
        sortable: true,
        sortValue: (server) =>
          (server.serverName || server.urlHost || server.canonicalServerUrl)
            .trim()
            .toLowerCase(),
        width: "2fr",
        render: (server) => <ShadowMCPInventoryServerCell server={server} />,
      },
      {
        key: "status",
        header: "Status",
        sortable: true,
        sortValue: (server) =>
          shadowMCPInventoryStatusLabel(inventoryAccessStatus(server)),
        width: "0.9fr",
        render: (server) => <StatusCell server={server} />,
      },
      {
        key: "lastCalled",
        header: "Last called",
        sortable: true,
        sortValue: (server) => server.lastCalled?.getTime() ?? 0,
        width: "0.7fr",
        render: (server) => (
          <Type variant="small">{formatShortDate(server.lastCalled)}</Type>
        ),
      },
      {
        key: "usage",
        header: "Usage",
        sortable: true,
        sortValue: (server) => server.observedUseCount,
        width: "0.5fr",
        render: (server) => <ShadowMCPInventoryUsageCell server={server} />,
      },
    ],
    [draftURLs, toggleURL],
  );

  const sortedServers = useMemo(
    () =>
      sortTableData(
        filteredServers,
        columns,
        sort,
      ) as ShadowMCPInventoryServer[],
    [columns, filteredServers, sort],
  );

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setDraftURLs(new Set(selectedURLs));
      setSearch("");
    }
    setOpen(nextOpen);
  };

  const applySelection = () => {
    onSelectionChange(new Set(draftURLs));
    setOpen(false);
  };

  const hasAppliedRows = appliedRows.length > 0;
  const showHeaderAction = hasAppliedRows || isLoading || error !== null;
  const headerActionLabel = hasAppliedRows
    ? "Manage servers"
    : "Select servers";

  let selectionContent: JSX.Element;
  if (isLoading) {
    selectionContent = (
      <Type muted small>
        Loading Shadow MCP inventory…
      </Type>
    );
  } else if (error) {
    selectionContent = (
      <div className="border-border bg-muted/20 flex items-center justify-between gap-4 rounded-md border px-4 py-3">
        <Type muted small>
          Shadow MCP inventory could not be loaded.
        </Type>
        <Button type="button" variant="tertiary" size="sm" onClick={onRetry}>
          Retry
        </Button>
      </div>
    );
  } else if (!hasAppliedRows) {
    selectionContent = (
      <EmptyServerSelection onSelect={() => handleOpenChange(true)} />
    );
  } else {
    selectionContent = <AppliedServerTable rows={appliedRows} />;
  }

  return (
    <section
      aria-labelledby="shadow-mcp-policy-server-selector-title"
      className="space-y-3"
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <Type
            id="shadow-mcp-policy-server-selector-title"
            variant="body"
            className="font-medium"
          >
            Servers allowed by this policy
          </Type>
          <Type muted small className="mt-1">
            These Shadow MCP servers remain available when the policy blocks
            access.
          </Type>
        </div>
        {showHeaderAction && (
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={isLoading || error !== null}
            onClick={() => handleOpenChange(true)}
          >
            {headerActionLabel}
          </Button>
        )}
      </div>

      {selectionContent}

      {hasAppliedRows && (
        <Type muted small>
          {selectedCountLabel(selectedURLs.size)}
        </Type>
      )}

      <Dialog open={open} onOpenChange={handleOpenChange}>
        <Dialog.Content
          className="max-h-[80vh] grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-5xl"
          onEscapeKeyDown={(event) => {
            if (search) {
              event.preventDefault();
              event.stopPropagation();
              setSearch("");
            }
          }}
        >
          <Dialog.Header>
            <Dialog.Title>Select allowed servers</Dialog.Title>
            <Dialog.Description>
              Choose which Shadow MCP servers remain available when this policy
              blocks access.
            </Dialog.Description>
          </Dialog.Header>

          <Input
            aria-label="Search servers"
            type="search"
            placeholder="Search by server name or URL"
            value={search}
            onChange={setSearch}
          />

          <Table columns={columns} className="min-h-0 overflow-hidden">
            <Table.Header
              columns={columns}
              sort={sort}
              onSortChange={setSort}
            />
            <Table.Body
              columns={columns}
              data={sortedServers}
              rowKey={(server) => server.canonicalServerUrl}
              onRowClick={(server) => toggleURL(server.canonicalServerUrl)}
              noResultsMessage={
                <Type muted small>
                  {selectorEmptyMessage(deferredSearch)}
                </Type>
              }
              className="min-h-0 content-start overflow-y-auto"
            />
          </Table>

          <Dialog.Footer className="items-center sm:justify-between">
            <Type muted small>
              {draftURLs.size} of {servers.length} servers selected
            </Type>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="tertiary"
                onClick={() => handleOpenChange(false)}
              >
                Cancel
              </Button>
              <Button type="button" variant="primary" onClick={applySelection}>
                Apply selection
              </Button>
            </div>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </section>
  );
}
