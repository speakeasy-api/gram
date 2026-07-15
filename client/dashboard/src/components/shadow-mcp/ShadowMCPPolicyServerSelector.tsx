import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { Badge, Button, type Column, Table } from "@speakeasy-api/moonshine";
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
      <Type variant="small" className="truncate font-medium">
        {serverLabel(server)}
      </Type>
      <Type muted small className="truncate text-xs">
        {server.canonicalServerUrl}
      </Type>
    </div>
  );
}

function ActivityCell({ server }: { server: ShadowMCPInventoryServer }) {
  return (
    <div className="space-y-1">
      <Type variant="small">{formatShortDate(server.lastSeen)}</Type>
      <Type muted small className="text-xs">
        Last called {formatShortDate(server.lastCalled)}
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
      <Type muted small className="text-xs">
        {countLabel(server.requestCount, "request", "requests")}
      </Type>
    </div>
  );
}

function AccessCell({ server }: { server: ShadowMCPInventoryServer }) {
  const status = inventoryAccessStatus(server);

  return (
    <Badge variant={shadowMCPInventoryStatusBadgeVariant(status)}>
      <Badge.Text>{shadowMCPInventoryStatusLabel(status)}</Badge.Text>
    </Badge>
  );
}

function AppliedServerList({
  servers,
}: {
  servers: ShadowMCPInventoryServer[];
}) {
  if (servers.length === 0) {
    return (
      <div className="border-border bg-muted/20 rounded-md border border-dashed px-4 py-5 text-center">
        <Type muted small>
          No servers selected
        </Type>
      </div>
    );
  }

  return (
    <div className="border-border divide-border divide-y overflow-hidden rounded-md border">
      {servers.map((server) => (
        <div
          key={server.canonicalServerUrl}
          className="flex items-center justify-between gap-4 px-3 py-2.5"
        >
          <ServerCell server={server} />
          <Type muted small className="shrink-0">
            {shadowMCPInventoryStatusLabel(inventoryAccessStatus(server))}
          </Type>
        </div>
      ))}
    </div>
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
        width: "1.5fr",
        render: (server) => <ServerCell server={server} />,
      },
      {
        key: "activity",
        header: "Last seen",
        width: "1fr",
        render: (server) => <ActivityCell server={server} />,
      },
      {
        key: "usage",
        header: "Usage",
        width: "0.7fr",
        render: (server) => <UsageCell server={server} />,
      },
      {
        key: "access",
        header: "Access",
        width: "0.6fr",
        render: (server) => <AccessCell server={server} />,
      },
    ],
    [draftURLs, toggleURL],
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
            Allowed Shadow MCP servers
          </Type>
          <Type muted small className="mt-1">
            Selected servers will remain available when this policy starts
            blocking.
          </Type>
        </div>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          disabled={isLoading || error !== null}
          onClick={() => handleOpenChange(true)}
        >
          Select servers
        </Button>
      </div>

      {isLoading ? (
        <Type muted small>
          Loading Shadow MCP inventory…
        </Type>
      ) : error ? (
        <div className="border-border bg-muted/20 flex items-center justify-between gap-4 rounded-md border px-4 py-3">
          <Type muted small>
            Shadow MCP inventory could not be loaded.
          </Type>
          <Button type="button" variant="tertiary" size="sm" onClick={onRetry}>
            Retry
          </Button>
        </div>
      ) : (
        <AppliedServerList servers={appliedServers} />
      )}

      <Type muted small>
        {selectedCountLabel(selectedURLs.size)}
      </Type>

      <Dialog open={open} onOpenChange={handleOpenChange}>
        <Dialog.Content className="max-h-[80vh] grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-5xl">
          <Dialog.Header>
            <Dialog.Title>Select allowed servers</Dialog.Title>
            <Dialog.Description>
              Choose the observed Shadow MCP servers that should remain
              available.
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
            <Table.Header columns={columns} />
            <Table.Body
              columns={columns}
              data={filteredServers}
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
