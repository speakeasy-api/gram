import { useMemo, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Link,
  Navigate,
  useNavigate,
  useParams,
  useSearch,
} from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";
import { ChevronRightIcon, FolderIcon } from "lucide-react";

import { CopyValue } from "@/components/CopyValue";
import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  organizationProjectsQuery,
  organizationQuery,
  projectMcpServersQuery,
} from "@/lib/adminQueries";
import type {
  AdminMcpServer,
  AdminOrganization,
  AdminProject,
} from "@/lib/gramAdminApi";
import { byOldestFirst, fmtDateShort } from "@/lib/utils";

import { SOURCE_LABELS, VISIBILITY_LABELS } from "./mcpServerLabels";

// `isPending`, not `isLoading`, for the reason `projectsMessage` in Projects
// gives: a paused read would otherwise fall through to "none".
function serversMessage(isPending: boolean, isError: boolean): string {
  if (isPending) return "Loading...";
  if (isError) return "Unable to load MCP servers";
  return "No MCP servers in this project";
}

const serverColumn = createColumnHelper<DataTableFeatures, AdminMcpServer>();

// Built per organization and project, because the name links to the server's
// health page and that address carries both.
function serverColumns(idOrSlug: string, project: string) {
  return serverColumn.columns([
    serverColumn.accessor("name", {
      header: "Name",
      // A floor, because the URL column takes the rest of the width and would
      // otherwise fold a two-word name onto three lines.
      meta: { cellClassName: "min-w-48 whitespace-normal" },
      // The link, not the row, carries the keyboard path and the accessible
      // name. It also lets the operator open the health page in a new tab.
      cell: ({ row }) => (
        <Link
          to="/organizations/$idOrSlug/mcp-servers/$serverId"
          params={{ idOrSlug, serverId: row.original.id }}
          search={{ project }}
          className="text-sm underline-offset-4 hover:underline focus-visible:underline"
        >
          {row.original.name}
        </Link>
      ),
    }),
    serverColumn.accessor("url", {
      header: "Server URL",
      // `max-w-0 w-full` gives the column whatever the others leave, and lets a
      // long URL truncate there rather than push them out of the border.
      meta: { cellClassName: "max-w-0 w-full" },
      cell: ({ row }) =>
        row.original.url ? (
          <CopyValue
            label={`${row.original.name} server URL`}
            value={row.original.url}
          />
        ) : (
          <span className="text-muted-foreground text-sm">-</span>
        ),
    }),
    serverColumn.accessor("visibility", {
      header: "Visibility",
      cell: ({ row }) => (
        <Badge
          variant={
            row.original.visibility === "public" ? "secondary" : "outline"
          }
        >
          {VISIBILITY_LABELS[row.original.visibility] ??
            row.original.visibility}
        </Badge>
      ),
    }),
    serverColumn.accessor("source", {
      header: "Source",
      cell: ({ row }) => (
        <span className="text-muted-foreground text-sm">
          {SOURCE_LABELS[row.original.source] ?? row.original.source}
        </span>
      ),
    }),
    serverColumn.accessor("created_at", {
      header: "Created",
      cell: ({ row }) => (
        <span className="text-sm">{fmtDateShort(row.original.created_at)}</span>
      ),
    }),
    serverColumn.display({
      id: "open",
      header: () => <span className="sr-only">Open</span>,
      cell: () => (
        <ChevronRightIcon
          aria-hidden="true"
          className="text-muted-foreground size-4"
        />
      ),
    }),
  ]);
}

export function McpServersRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <McpServers org={data} />;
}

export function McpServers({ org }: { org: AdminOrganization }): JSX.Element {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const navigate = useNavigate({
    from: "/organizations/$idOrSlug/mcp-servers/",
  });
  const { project: requested } = useSearch({
    from: "/organizations/$idOrSlug/mcp-servers/",
  });
  const projectsRead = useQuery(organizationProjectsQuery(org.id));

  const projects = useMemo(
    () => [...(projectsRead.data?.projects ?? [])].sort(byOldestFirst),
    [projectsRead.data],
  );

  // Always a project once they have loaded. An id that is missing, unknown or
  // another organization's reads as the oldest project.
  const selected: AdminProject | undefined =
    projects.find((p) => p.id === requested) ?? projects[0];

  const serversRead = useQuery({
    ...projectMcpServersQuery(org.id, selected?.id ?? ""),
    enabled: !!selected,
  });

  const selectedId = selected?.id ?? "";
  const columns = useMemo(
    () => serverColumns(idOrSlug, selectedId),
    [idOrSlug, selectedId],
  );

  const table = useTable({
    features: dataTableFeatures,
    columns,
    data: serversRead.data?.mcp_servers ?? EMPTY,
    getRowId: (server) => server.id,
  });

  const rows = table.getRowModel().rows;

  if (projectsRead.isPending) {
    return <span className="text-muted-foreground text-sm">Loading...</span>;
  }
  if (projectsRead.isError) {
    return (
      <span className="text-muted-foreground text-sm">
        Unable to load projects
      </span>
    );
  }
  if (!selected) {
    return (
      <span className="text-muted-foreground text-sm">
        No projects in this organization
      </span>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {/* The address is corrected in place, so Back does not return to an id
          that named nothing. */}
      {selected.id !== requested && (
        <Navigate
          from="/organizations/$idOrSlug/mcp-servers/"
          search={{ project: selected.id }}
          replace
        />
      )}
      <div className="flex items-center gap-3">
        <Select
          value={selected.id}
          onValueChange={(project) => {
            // Replaced, so Back leaves the page rather than stepping through
            // every project viewed.
            void navigate({ search: { project }, replace: true });
          }}
        >
          <SelectTrigger size="sm" aria-label="Project" className="bg-card">
            <FolderIcon />
            <span className="text-muted-foreground">Project</span>
            <SelectValue>
              <span className="font-medium">{selected.name}</span>
            </SelectValue>
          </SelectTrigger>
          <SelectContent position="popper" align="start" className="min-w-64">
            {projects.map((project) => (
              <SelectItem
                key={project.id}
                value={project.id}
                // Stretches the item's text so the count sits at the right
                // edge, clear of the check.
                className="*:[span]:last:flex-1"
              >
                <span className="grow">{project.name}</span>
                <span className="text-muted-foreground text-xs tabular-nums">
                  {project.mcp_server_count}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {rows.length > 0 && (
          <p className="text-sm font-medium">
            {rows.length === 1 ? "1 MCP server" : `${rows.length} MCP servers`}
          </p>
        )}
      </div>
      <div className="overflow-clip rounded-lg border">
        <Table cellPadding="condensed">
          <Table.Header table={table} />
          <Table.Body>
            {serversRead.isPending || rows.length === 0 ? (
              <Table.NoResultsMessage>
                <span className="text-muted-foreground text-sm">
                  {serversMessage(serversRead.isPending, serversRead.isError)}
                </span>
              </Table.NoResultsMessage>
            ) : (
              rows.map((row) => (
                <Table.Row
                  key={row.id}
                  row={row}
                  // The copy button is a control, so the row leaves its
                  // click alone and copying never navigates.
                  onClick={(server) => {
                    void navigate({
                      to: "/organizations/$idOrSlug/mcp-servers/$serverId",
                      params: { idOrSlug, serverId: server.id },
                      search: { project: selected.id },
                    });
                  }}
                />
              ))
            )}
          </Table.Body>
        </Table>
      </div>
    </div>
  );
}

// One array for every empty render, so the table does not rebuild its row
// model against a fresh `[]` each time.
const EMPTY: AdminMcpServer[] = [];
