import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { type Column, type SortDescriptor, Table } from "@/components/ui/Table";
import { sortTableData } from "@/components/ui/Table/sorting";
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

type ShadowMCPPolicyServerSelectorMode = "allow" | "block";

export type ShadowMCPPolicyServerSelectorProps = {
  servers: ShadowMCPInventoryServer[];
  originalURLs: ReadonlySet<string>;
  selectedURLs: ReadonlySet<string>;
  onSelectionChange: (next: Set<string>) => void;
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
  /** "allow" (block_all policies — pick servers that stay available) or
   * "block" (allow_all policies — pick servers that get denied). */
  mode?: ShadowMCPPolicyServerSelectorMode;
};

const SELECTOR_COPY: Record<
  ShadowMCPPolicyServerSelectorMode,
  {
    title: string;
    subtitle: string;
    emptyTitle: string;
    emptySubtitle: string;
    dialogTitle: string;
    dialogDescription: string;
  }
> = {
  allow: {
    title: "Servers allowed by this policy",
    subtitle:
      "These Shadow MCP servers remain available when the policy blocks access.",
    emptyTitle: "No servers allowed yet",
    emptySubtitle:
      "Select any Shadow MCP servers that should remain available when this policy blocks access.",
    dialogTitle: "Select allowed servers",
    dialogDescription:
      "Choose which Shadow MCP servers remain available when this policy blocks access.",
  },
  block: {
    title: "Servers blocked by this policy",
    subtitle:
      "These Shadow MCP servers are denied while every other server stays available.",
    emptyTitle: "No servers blocked yet",
    emptySubtitle:
      "Select any Shadow MCP servers that should be denied while this policy allows everything else.",
    dialogTitle: "Select blocked servers",
    dialogDescription:
      "Choose which Shadow MCP servers are denied while this policy allows everything else.",
  },
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

function EmptyServerSelection({
  onSelect,
  title,
  subtitle,
}: {
  onSelect: () => void;
  title: string;
  subtitle: string;
}) {
  return (
    <div className="bg-muted/20 flex flex-col items-center justify-center border border-dashed px-6 py-8 text-center">
      <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
        <Icon
          aria-hidden="true"
          name="shield-check"
          className="text-muted-foreground h-6 w-6"
        />
      </div>
      <Text variant="subheading" className="mb-1">
        {title}
      </Text>
      <Text small muted className="mb-4 max-w-md">
        {subtitle}
      </Text>
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
        <Text variant="small" className="truncate font-medium" title={label}>
          {label}
        </Text>
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
      <Text
        muted
        small
        className="truncate font-mono text-xs"
        title={server.canonicalServerUrl}
      >
        {server.canonicalServerUrl}
      </Text>
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
  mode = "allow",
}: ShadowMCPPolicyServerSelectorProps): JSX.Element {
  const copy = SELECTOR_COPY[mode];
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
          <Text variant="small">{formatShortDate(server.lastCalled)}</Text>
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
      <Text muted small>
        Loading Shadow MCP inventory…
      </Text>
    );
  } else if (error) {
    selectionContent = (
      <div className="border-border bg-muted/20 flex items-center justify-between gap-4 border px-4 py-3">
        <Text muted small>
          Shadow MCP inventory could not be loaded.
        </Text>
        <Button type="button" variant="tertiary" size="sm" onClick={onRetry}>
          Retry
        </Button>
      </div>
    );
  } else if (!hasAppliedRows) {
    selectionContent = (
      <EmptyServerSelection
        onSelect={() => handleOpenChange(true)}
        title={copy.emptyTitle}
        subtitle={copy.emptySubtitle}
      />
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
          <Text
            id="shadow-mcp-policy-server-selector-title"
            variant="body"
            className="font-medium"
          >
            {copy.title}
          </Text>
          <Text muted small className="mt-1">
            {copy.subtitle}
          </Text>
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
        <Text muted small>
          {selectedCountLabel(selectedURLs.size)}
        </Text>
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
            <Dialog.Title>{copy.dialogTitle}</Dialog.Title>
            <Dialog.Description>{copy.dialogDescription}</Dialog.Description>
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
                <Text muted small>
                  {selectorEmptyMessage(deferredSearch)}
                </Text>
              }
              className="min-h-0 content-start overflow-y-auto"
            />
          </Table>

          <Dialog.Footer className="items-center sm:justify-between">
            <Text muted small>
              {draftURLs.size} of {servers.length} servers selected
            </Text>
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
