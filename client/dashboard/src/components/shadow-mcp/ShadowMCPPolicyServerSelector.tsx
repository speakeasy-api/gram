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
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusLabel,
  type ShadowMCPInventoryStatus,
} from "./shadowMCPInventoryStatus";

export type ShadowMCPPolicyServerSelectorProps = {
  servers: ShadowMCPInventoryServer[];
  selectedURLs: ReadonlySet<string>;
  onSelectionChange: (next: Set<string>) => void;
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
};

function countLabel(count: number, singular: string, plural: string): string {
  return `${count} ${count === 1 ? singular : plural}`;
}

function selectedCountLabel(count: number): string {
  return countLabel(count, "server selected", "servers selected");
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

function ServerCell({ server }: { server: ShadowMCPInventoryServer }) {
  return (
    <div className="min-w-0 space-y-1">
      <div className="flex items-center gap-2">
        <Type variant="small" className="truncate font-medium">
          {serverLabel(server)}
        </Type>
        {server.requestCount > 0 && (
          <Badge variant="warning" size="sm" background={false}>
            <Badge.LeftIcon>
              <Icon name="shield-alert" />
            </Badge.LeftIcon>
            <Badge.Text>
              {server.requestCount} Access Request
              {server.requestCount > 1 && "s"}
            </Badge.Text>
          </Badge>
        )}
      </div>
      <Type muted small className="truncate text-xs">
        {server.canonicalServerUrl}
      </Type>
    </div>
  );
}

function UsageCell({ server }: { server: ShadowMCPInventoryServer }) {
  return (
    <div className="space-y-1">
      <Type variant="small">
        {countLabel(server.observedUseCount, "call", "calls")}
      </Type>
      <Type muted small className="text-xs">
        {countLabel(server.userCount, "user", "users")}
      </Type>
    </div>
  );
}

function StatusCell({ server }: { server: ShadowMCPInventoryServer }) {
  const status = inventoryAccessStatus(server);

  return (
    <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
      <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
    </Badge>
  );
}

function AppliedServerRow({ server }: { server: ShadowMCPInventoryServer }) {
  const label = serverLabel(server);

  return (
    <li
      data-testid="applied-shadow-mcp-server"
      className="grid h-9 grid-cols-[minmax(7rem,0.35fr)_minmax(0,1fr)] items-center gap-3 px-3"
    >
      <Type variant="small" className="truncate font-medium" title={label}>
        {label}
      </Type>
      <Type
        muted
        small
        className="truncate font-mono text-xs"
        title={server.canonicalServerUrl}
      >
        {server.canonicalServerUrl}
      </Type>
    </li>
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

function AppliedServerList({
  servers,
}: {
  servers: ShadowMCPInventoryServer[];
}) {
  if (servers.length === 0) {
    return (
      <Type muted small>
        No servers selected
      </Type>
    );
  }

  return (
    <ul
      aria-label="Selected Shadow MCP servers"
      tabIndex={0}
      data-testid="applied-shadow-mcp-servers"
      className="border-border divide-border focus-visible:ring-ring max-h-[200px] divide-y overflow-y-auto rounded-md border focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      {servers.map((server) => (
        <AppliedServerRow key={server.canonicalServerUrl} server={server} />
      ))}
    </ul>
  );
}

export function ShadowMCPPolicyServerSelector({
  servers,
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

  const appliedServers = useMemo(
    () =>
      servers.filter((server) => selectedURLs.has(server.canonicalServerUrl)),
    [selectedURLs, servers],
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
        render: (server) => <ServerCell server={server} />,
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
        render: (server) => <UsageCell server={server} />,
      },
    ],
    [draftURLs, toggleURL],
  );

  const sortedServers = sortTableData(
    filteredServers,
    columns,
    sort,
  ) as ShadowMCPInventoryServer[];

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

  const showHeaderAction = selectedURLs.size > 0 || isLoading || error !== null;
  const headerActionLabel =
    selectedURLs.size > 0 ? "Manage servers" : "Select servers";

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
  } else if (selectedURLs.size === 0) {
    selectionContent = (
      <EmptyServerSelection onSelect={() => handleOpenChange(true)} />
    );
  } else {
    selectionContent = <AppliedServerList servers={appliedServers} />;
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

      {selectedURLs.size > 0 && (
        <Type muted small>
          {selectedCountLabel(selectedURLs.size)}
        </Type>
      )}

      <Dialog open={open} onOpenChange={handleOpenChange}>
        <Dialog.Content className="max-h-[80vh] grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-5xl">
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
                  No matching servers
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
