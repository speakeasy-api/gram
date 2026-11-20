import { useMemo, useRef, type JSX } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";
import { toast } from "sonner";

import { useConfirmDialog } from "@/components/ConfirmDialog";
import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import { Button } from "@/components/ui/button";
import {
  invalidateProjects,
  organizationProjectsQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import {
  errorMessage,
  getProjectRedeployPreview,
  redeployProject,
  type AdminOrganization,
  type AdminProject,
  type AdminProjectRedeployPreview,
  type RedeployProjectRequest,
} from "@/lib/gramAdminApi";
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

function countedLine(count: number, one: string, many: string): string {
  return `${count} ${count === 1 ? one : many}`;
}

function RedeployDetails({
  preview,
}: {
  preview: AdminProjectRedeployPreview;
}): JSX.Element {
  return (
    <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
      <li>
        Clones deployment{" "}
        <span className="font-mono break-all">
          {preview.active_deployment_id}
        </span>
      </li>
      <li>
        {countedLine(
          preview.openapi_assets,
          "OpenAPI source is processed again",
          "OpenAPI sources are processed again",
        )}
      </li>
      <li>
        {countedLine(
          preview.function_assets,
          "function source redeploys its runner",
          "function sources redeploy their runners",
        )}
      </li>
      <li>
        {countedLine(
          preview.packages,
          "package carries over",
          "packages carry over",
        )}
      </li>
      <li>The customer sees this in their deployments and audit log</li>
    </ul>
  );
}

function RedeployProjectButton({
  organizationId,
  project,
}: {
  organizationId: string;
  project: AdminProject;
}): JSX.Element {
  const qc = useQueryClient();
  const [confirm, confirmDialog] = useConfirmDialog();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const mut = useMutation({
    mutationFn: (request: RedeployProjectRequest) => redeployProject(request),
    onSuccess: () => invalidateProjects(qc),
  });

  const redeploy = async (): Promise<void> => {
    try {
      const preview = await getProjectRedeployPreview(
        organizationId,
        project.id,
      );
      const confirmed = await confirm({
        title: `Redeploy ${project.name}?`,
        description:
          "The current deployment stays active until the new one succeeds, and stays active if it fails.",
        details: <RedeployDetails preview={preview} />,
        confirmLabel: "Redeploy",
      });
      // The dialog has no opener to give the keyboard back to on its own.
      buttonRef.current?.focus();
      if (!confirmed) return;

      const result = await mut.mutateAsync({
        organization_id: organizationId,
        project_id: project.id,
        expected_active_deployment_id: preview.active_deployment_id,
      });
      toast.success("Redeploy started", { description: result.deployment_id });
    } catch (error) {
      toast.error(errorMessage(error));
    }
  };

  // React events cross portals, so without this a click inside the dialog would
  // reach the row and open the project.
  return (
    <div
      role="presentation"
      onClick={(event) => {
        event.stopPropagation();
      }}
    >
      <Button
        ref={buttonRef}
        variant="outline"
        size="sm"
        disabled={mut.isPending}
        aria-label={`Redeploy ${project.name}`}
        onClick={() => {
          void redeploy();
        }}
      >
        Redeploy
      </Button>
      {confirmDialog}
    </div>
  );
}

// The column set is one record's projects, so the organization is the same for
// every row and the link takes it from the route rather than from the row.
function projectColumns(idOrSlug: string, organizationId: string) {
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
    projectColumn.display({
      id: "actions",
      header: "Actions",
      enableHiding: false,
      meta: {
        headClassName: "sticky right-0 z-1 w-px bg-muted",
        cellClassName: "sticky right-0 z-1 w-px bg-inherit",
      },
      cell: ({ row }) => (
        <RedeployProjectButton
          organizationId={organizationId}
          project={row.original}
        />
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
  const columns = useMemo(
    () => projectColumns(idOrSlug, org.id),
    [idOrSlug, org.id],
  );

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
