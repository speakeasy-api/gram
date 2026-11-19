import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam } from "@/lib/sources";
import type { CatalogServer } from "@/pages/catalog/hooks";
import { shadowMCPCatalogMatch } from "@/components/shadow-mcp/shadowMCPOfficialServer";
import { useRoutes } from "@/routes";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { Plus } from "lucide-react";
import { useMemo } from "react";
import { Link, useNavigate } from "react-router";
import {
  remoteMcpPrefillHref,
  teamServerExistingServer,
  teamServerLabel,
} from "./team-usage";

/** The URL without its scheme, which is what people recognize a server by. */
function displayUrl(raw: string): string {
  return raw.replace(/^https?:\/\//, "").replace(/\/+$/, "");
}

/**
 * The servers the team connects to directly, most used first, each with the
 * one step that brings it into Speakeasy: open the project server that
 * already serves it, add its catalog entry, or create a remote server with
 * its URL filled in.
 */
export function TeamUsageTable({
  servers,
  catalog,
  catalogLoading,
  search,
  isLoading,
  isError,
  onRetry,
  onAddFromCatalog,
}: {
  /** Already filtered to hostable servers and sorted by usage. */
  servers: ShadowMCPInventoryServer[];
  catalog: CatalogServer[];
  /**
   * Whether the catalog is still loading. Add waits for it, since which flow
   * Add opens depends on whether a catalog entry serves the URL.
   */
  catalogLoading: boolean;
  search: string;
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  onAddFromCatalog: (server: CatalogServer) => void;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();

  const normalizedSearch = search.trim().toLowerCase();
  const visible = useMemo(() => {
    if (normalizedSearch.length === 0) return servers;
    return servers.filter((server) =>
      [server.serverName, server.urlHost, server.canonicalServerUrl].some(
        (value) => value?.toLowerCase().includes(normalizedSearch),
      ),
    );
  }, [normalizedSearch, servers]);

  const columns: Column<ShadowMCPInventoryServer>[] = [
    {
      key: "server",
      header: "Server",
      width: "2fr",
      render: (server) => (
        <div className="min-w-0 space-y-1">
          <Text variant="small" className="block truncate font-medium">
            {teamServerLabel(server)}
          </Text>
          <Text
            muted
            small
            className="block truncate font-mono text-xs"
            title={server.canonicalServerUrl}
          >
            {displayUrl(server.canonicalServerUrl)}
          </Text>
        </div>
      ),
    },
    {
      key: "userCount",
      header: "Users",
      width: "0.6fr",
      render: (server) => (
        <Text variant="small">{server.userCount.toLocaleString()}</Text>
      ),
    },
    {
      key: "observedUseCount",
      header: "Calls",
      width: "0.6fr",
      render: (server) => (
        <Text variant="small">{server.observedUseCount.toLocaleString()}</Text>
      ),
    },
    {
      key: "lastCalled",
      header: "Last used",
      width: "0.8fr",
      render: (server) => (
        <Text variant="small" muted>
          {formatShortDate(server.lastCalled ?? server.lastSeen)}
        </Text>
      ),
    },
    {
      key: "canonicalServerUrl",
      id: "action",
      header: "",
      width: "150px",
      render: (server) => {
        const existing = teamServerExistingServer(server);
        if (existing) {
          return (
            <div className="flex items-center justify-end gap-3">
              <Text muted small>
                Added
              </Text>
              <Button variant="tertiary" size="sm" asChild>
                <Link
                  to={routes.mcp.details.overview.href(
                    mcpServerRouteParam({
                      id: existing.mcpServerId,
                      slug: existing.slug,
                    }),
                  )}
                >
                  <Button.Text>Open</Button.Text>
                </Link>
              </Button>
            </div>
          );
        }
        const catalogEntry = shadowMCPCatalogMatch(
          catalog,
          server.canonicalServerUrl,
        );
        return (
          <div className="flex justify-end">
            <Button
              variant="secondary"
              size="sm"
              aria-label={`Add ${teamServerLabel(server)}`}
              disabled={catalogLoading}
              onClick={() => {
                if (catalogEntry) {
                  onAddFromCatalog(catalogEntry);
                  return;
                }
                void navigate(
                  remoteMcpPrefillHref(routes.mcp.add.remote.href(), server),
                );
              }}
            >
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Add</Button.Text>
            </Button>
          </div>
        );
      },
    },
  ];

  if (isLoading) {
    return <SkeletonTable />;
  }

  if (isError) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load the servers your team uses"
        description="It may be a temporary problem."
        action={
          <Button variant="secondary" size="sm" onClick={onRetry}>
            <Button.Text>Retry</Button.Text>
          </Button>
        }
      />
    );
  }

  if (servers.length === 0) {
    return (
      <InlineEmptyState
        icon="users"
        heading="No servers to suggest yet"
        description="When your team's agents connect to remote MCP servers on their own, the ones Speakeasy can host show up here, most used first."
        action={
          <Button variant="secondary" size="sm" asChild>
            <Link to={routes.shadowMCP.href()}>
              <Button.Text>Open Shadow MCP</Button.Text>
            </Link>
          </Button>
        }
      />
    );
  }

  return (
    <div className="space-y-3">
      <Text muted small className="block">
        Remote servers your team&apos;s agents connect to directly, most used
        first. Users and calls count everything Speakeasy has seen.
      </Text>
      <Table
        columns={columns}
        data={visible}
        rowKey={(server) => server.canonicalServerUrl}
        noResultsMessage={`No servers matching “${search.trim()}”`}
      />
    </div>
  );
}
