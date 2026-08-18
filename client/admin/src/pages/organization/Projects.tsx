import { useMemo, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";

import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import {
  organizationProjectsQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import type { AdminOrganization, AdminProject } from "@/lib/gramAdminApi";
import { byOldestFirst, fmtDateShort } from "@/lib/utils";

// `isPending`, not `isLoading`: React Query makes the second of those
// `isPending && isFetching`, so a paused read is neither loading nor errored and
// falls through to the sentence that says the organization has none.
function projectsMessage(isPending: boolean, isError: boolean): string {
  if (isPending) return "Loading...";
  if (isError) return "Unable to load projects";
  return "No projects in this organization";
}

const projectColumn = createColumnHelper<DataTableFeatures, AdminProject>();

// The column set is one record's projects, so the organization is the same for
// every row and the link takes it from the route rather than from the row.
function projectColumns(idOrSlug: string) {
  return projectColumn.columns([
    projectColumn.accessor("name", {
      header: "Name",
      // Wrapping, against the table's own `whitespace-nowrap`, for the reason
      // the Members view gives: the border clips rather than scrolls, so a
      // squeezed column folds its text instead of hiding it.
      meta: { cellClassName: "whitespace-normal" },
      // The link, not the row, carries the keyboard path and the accessible
      // name. It also lets the operator open the project in a new tab.
      //
      // Always the id. Project slugs are unique only within an organization, so
      // project.get resolves a slug across all of them, and "default" matches
      // one project in every organization.
      cell: ({ row }) => (
        <Link
          to="/organizations/$idOrSlug/projects/$projectIdOrSlug"
          params={{
            idOrSlug,
            projectIdOrSlug: row.original.id,
          }}
          className="text-sm underline-offset-4 hover:underline focus-visible:underline"
        >
          {row.original.name}
        </Link>
      ),
    }),
    projectColumn.accessor("slug", {
      header: "Slug",
      // `break-all`, because a slug is one word to a word-breaking rule.
      meta: { cellClassName: "whitespace-normal break-all" },
      cell: ({ row }) => <span className="text-sm">{row.original.slug}</span>,
    }),
    projectColumn.accessor("mcp_server_count", {
      // "MCP Servers", not "Toolsets": the count is both server models, the
      // toolset-backed legacy one and the `mcp_servers` row. AGE-3276.
      header: "MCP Servers",
      // Printed even when it is zero. A blank cell reads as a project nobody
      // has measured, and none is a real answer about a project that has been.
      cell: ({ row }) => (
        <span className="text-sm tabular-nums">
          {row.original.mcp_server_count}
        </span>
      ),
    }),
    projectColumn.accessor("created_at", {
      header: "Created",
      cell: ({ row }) => (
        <span className="text-sm">{fmtDateShort(row.original.created_at)}</span>
      ),
    }),
  ]);
}

export function ProjectsRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Projects idOrSlug={idOrSlug} org={data} />;
}

// `idOrSlug` is the address the operator is on, not `org.slug`. Rewriting it
// would move the record to another cache entry on the next link press.
export function Projects({
  idOrSlug,
  org,
}: {
  idOrSlug: string;
  org: AdminOrganization;
}): JSX.Element {
  const navigate = useNavigate();
  const { data, isPending, isError } = useQuery({
    ...organizationProjectsQuery(org.id),
    enabled: !!org.id,
  });

  // Sorted here rather than trusted from the endpoint, which promises no order,
  // and a fresh array each render would rebuild the row model every time.
  const projects = useMemo(
    () => [...(data?.projects ?? [])].sort(byOldestFirst),
    [data],
  );

  // Rebuilt only when the record changes. A fresh column set each render
  // rebuilds the row model with it.
  const columns = useMemo(() => projectColumns(idOrSlug), [idOrSlug]);

  const table = useTable({
    features: dataTableFeatures,
    columns,
    data: projects,
    getRowId: (project) => project.id,
  });

  const rows = table.getRowModel().rows;

  return (
    <div className="flex flex-col gap-3">
      {/* Only once the read has answered with rows. A count over a pending read
          is a number nobody has established, and "0 projects" beside the
          sentence that says there are none says it twice. */}
      {rows.length > 0 && (
        <p className="text-sm font-medium">
          {rows.length === 1 ? "1 project" : `${rows.length} projects`}
        </p>
      )}
      {/* `overflow-clip`, not `overflow-hidden`: hidden makes this a scroll
          container and the `sticky top-0` header would pin to it, not the page. */}
      <div className="overflow-clip rounded-lg border">
        <Table cellPadding="condensed">
          <Table.Header table={table} />
          <Table.Body>
            {isPending || rows.length === 0 ? (
              <Table.NoResultsMessage>
                <span className="text-muted-foreground text-sm">
                  {projectsMessage(isPending, isError)}
                </span>
              </Table.NoResultsMessage>
            ) : (
              rows.map((row) => (
                <Table.Row
                  key={row.id}
                  row={row}
                  onClick={(project) => {
                    void navigate({
                      to: "/organizations/$idOrSlug/projects/$projectIdOrSlug",
                      params: {
                        idOrSlug,
                        projectIdOrSlug: project.id,
                      },
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
