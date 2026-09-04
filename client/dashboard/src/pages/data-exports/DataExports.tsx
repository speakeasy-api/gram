import { InlineEmptyState } from "@/components/inline-empty-state";
import { SettingsPage } from "@/components/page-templates";
import { ProjectAvatar } from "@/components/project-menu";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { MoreActions } from "@/components/ui/MoreActions";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { toError } from "@/lib/errors";
import { writeOnlyHeaderInput } from "@/lib/write-only-headers";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DataSource,
  type DataSource as DataSourceValue,
} from "@gram/client/models/components/createdataexportrouteform.js";
import type { DataExportRoute } from "@gram/client/models/components/dataexportroute.js";
import type { ListDataExportsForOrgResult } from "@gram/client/models/components/listdataexportsfororgresult.js";
import type { ProjectEntry } from "@gram/client/models/components/projectentry.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useCreateDataExportRouteMutation } from "@gram/client/react-query/createDataExportRoute.js";
import { useCreateDataExportDestinationMutation } from "@gram/client/react-query/createDataExportDestination.js";
import { invalidateDataExportRoutes } from "@gram/client/react-query/dataExportRoutes.js";
import { useDeleteDataExportRouteMutation } from "@gram/client/react-query/deleteDataExportRoute.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { invalidateDataExportDestinations } from "@gram/client/react-query/dataExportDestinations.js";
import { buildListDataExportsForOrgQuery } from "@gram/client/react-query/listDataExportsForOrg.js";
import { useUpdateDataExportDestinationMutation } from "@gram/client/react-query/updateDataExportDestination.js";
import { useUpdateDataExportRouteMutation } from "@gram/client/react-query/updateDataExportRoute.js";
import { useId, useState } from "react";
import { toast } from "sonner";
import {
  ConfigureDestinationSheet,
  type ConfigureDestinationValues,
} from "./ConfigureDestinationSheet";
import {
  ConfigureExportSheet,
  type ConfigureExportValues,
  type OtelDataExportDestination,
} from "./ConfigureExportSheet";

const EMPTY_PROJECTS: ProjectEntry[] = [];
const EMPTY_DESTINATIONS: OtelDataExportDestination[] = [];
const EMPTY_ROUTES: DataExportRoute[] = [];

const DATA_SOURCE_OPTIONS: Array<{
  value: DataSourceValue;
  label: string;
  description: string;
}> = [
  {
    value: DataSource.ProductTelemetry,
    label: "Product telemetry",
    description:
      "OTLP traces, logs, and metrics from MCP servers and tool calls.",
  },
  {
    value: DataSource.RiskFindings,
    label: "Risk findings",
    description: "OTLP logs for findings detected by Gram risk scanners.",
  },
];

type ExportRow = {
  route: DataExportRoute;
  destination?: OtelDataExportDestination;
};

type ProjectExportRow = ExportRow & {
  project: ProjectEntry;
};

type VisualDestination = {
  key: string;
  type: "OTEL";
  name: string;
  detail: string;
  sensitive: boolean;
};

function visualDestination({
  route,
  destination,
}: ProjectExportRow): VisualDestination {
  return {
    key: route.otelDestinationId ?? `${route.id}-otel`,
    type: "OTEL",
    name: destination?.name ?? "Not configured",
    detail:
      destination?.otel.endpointUrl ?? "Configure this export to continue",
    sensitive: destination?.sensitiveData === "include",
  };
}

type VisualSource = {
  key: string;
  name: string;
  detail: string;
};

function visualSource({ route }: ProjectExportRow): VisualSource {
  return {
    key: route.id,
    name: sourceLabel(route.dataSource),
    detail: sourceDescription(route.dataSource),
  };
}

type ProjectExports = {
  project: ProjectEntry;
  destinations: OtelDataExportDestination[];
  routes: DataExportRoute[];
  exports: ExportRow[];
  pending: boolean;
  error: unknown;
};

type DeleteCandidate = {
  project: ProjectEntry;
  route: DataExportRoute;
};
type DestinationEditCandidate = {
  project: ProjectEntry;
  destination: OtelDataExportDestination;
};

function sourceLabel(dataSource: string): string {
  return (
    DATA_SOURCE_OPTIONS.find((option) => option.value === dataSource)?.label ??
    dataSource.replaceAll("_", " ")
  );
}

function sourceDescription(dataSource: string): string {
  return (
    DATA_SOURCE_OPTIONS.find((option) => option.value === dataSource)
      ?.description ?? "OTLP data"
  );
}

export default function DataExports(): JSX.Element {
  return (
    <RequireScope scope="org:read" level="page">
      <DataExportsInner />
    </RequireScope>
  );
}

function DataExportsInner(): JSX.Element {
  const organization = useOrganization();
  const client = useGramContext();
  const queryClient = useQueryClient();
  const projectsQuery = useListProjects({ organizationId: organization.id });
  const aggregateQuery = buildListDataExportsForOrgQuery(client);
  const aggregateQueryKey = [
    ...aggregateQuery.queryKey,
    { organizationId: organization.id },
  ];
  const exportsQuery = useQuery({
    ...aggregateQuery,
    queryKey: aggregateQueryKey,
    throwOnError: false,
  });
  const projects = projectsQuery.data?.projects ?? EMPTY_PROJECTS;
  const destinationsByProjectID = new Map<
    string,
    OtelDataExportDestination[]
  >();
  for (const destination of exportsQuery.data?.destinations ?? []) {
    if (
      destination.destinationType !== "otel" ||
      destination.otel === undefined
    ) {
      continue;
    }
    const destinations =
      destinationsByProjectID.get(destination.projectId) ?? [];
    destinations.push(destination as OtelDataExportDestination);
    destinationsByProjectID.set(destination.projectId, destinations);
  }
  const routesByProjectID = new Map<string, DataExportRoute[]>();
  for (const route of exportsQuery.data?.routes ?? []) {
    const routes = routesByProjectID.get(route.projectId) ?? [];
    routes.push(route);
    routesByProjectID.set(route.projectId, routes);
  }
  const projectExports: ProjectExports[] = projects.map((project) => {
    const destinations =
      destinationsByProjectID.get(project.id) ?? EMPTY_DESTINATIONS;
    const routes = routesByProjectID.get(project.id) ?? EMPTY_ROUTES;
    const destinationByID = new Map(
      destinations.map((destination) => [destination.id, destination]),
    );
    return {
      project,
      destinations,
      routes,
      exports: routes.map((route) => ({
        route,
        destination: route.otelDestinationId
          ? destinationByID.get(route.otelDestinationId)
          : undefined,
      })),
      pending: exportsQuery.isPending,
      error: exportsQuery.error,
    };
  });

  const orderedProjectExports = projectExports.toSorted(
    (left, right) =>
      Number(right.exports.length > 0) - Number(left.exports.length > 0),
  );
  const configuredExports: ProjectExportRow[] = orderedProjectExports.flatMap(
    (state) =>
      state.exports.map((item) => ({ ...item, project: state.project })),
  );
  const availableProjectExports = orderedProjectExports.filter(
    (state) =>
      !state.pending &&
      !state.error &&
      DATA_SOURCE_OPTIONS.some(
        (source) =>
          !state.routes.some((route) => route.dataSource === source.value),
      ),
  );
  const projectQueriesPending = exportsQuery.isPending;
  const projectQueryError = exportsQuery.error;

  const createDestination = useCreateDataExportDestinationMutation();
  const updateDestination = useUpdateDataExportDestinationMutation();
  const createRoute = useCreateDataExportRouteMutation();
  const updateRoute = useUpdateDataExportRouteMutation();
  const deleteRoute = useDeleteDataExportRouteMutation();
  const [configureTarget, setConfigureTarget] = useState<{
    projectSlug: string;
    routeId?: string;
  }>();
  const [deleteCandidate, setDeleteCandidate] = useState<DeleteCandidate>();
  const [destinationEditCandidate, setDestinationEditCandidate] =
    useState<DestinationEditCandidate>();
  const configureState = projectExports.find(
    ({ project }) => project.slug === configureTarget?.projectSlug,
  );
  const configureRoute = configureState?.routes.find(
    (route) => route.id === configureTarget?.routeId,
  );
  const configureDataSources = configureRoute
    ? DATA_SOURCE_OPTIONS.filter(
        (source) => source.value === configureRoute.dataSource,
      )
    : DATA_SOURCE_OPTIONS.filter(
        (source) =>
          !configureState?.routes.some(
            (route) => route.dataSource === source.value,
          ),
      );
  const configureProjects =
    configureState && configureRoute
      ? [configureState.project]
      : availableProjectExports.map(({ project }) => project);
  const mutating =
    createDestination.isPending ||
    updateDestination.isPending ||
    createRoute.isPending ||
    updateRoute.isPending ||
    deleteRoute.isPending;

  const invalidateProjectExports = async (projectSlug: string) =>
    Promise.all([
      invalidateDataExportDestinations(queryClient, [
        { gramProject: projectSlug },
      ]),
      invalidateDataExportRoutes(queryClient, [{ gramProject: projectSlug }]),
      queryClient.invalidateQueries({ queryKey: aggregateQueryKey }),
    ]);

  const handleSaveExport = async (values: ConfigureExportValues) => {
    const state = projectExports.find(
      ({ project }) => project.slug === values.projectSlug,
    );
    if (!state) return;

    try {
      const existingDestination = state.destinations.find(
        (destination) => destination.id === values.destinationId,
      );
      let destinationID = existingDestination?.id;
      if (!destinationID) {
        const destination = await createDestination.mutateAsync({
          request: {
            gramProject: state.project.slug,
            createDestinationForm: {
              destinationType: "otel",
              name: values.destinationName.trim(),
              sensitiveData: values.includeSensitiveData
                ? "include"
                : "exclude",
              otel: {
                endpointUrl: values.endpointUrl.trim(),
                headers: values.headers.map((header) => ({
                  name: header.name.trim(),
                  value: header.value,
                })),
              },
            },
          },
        });
        destinationID = destination.id;
      }

      const existingRoute = state.routes.find(
        (route) => route.dataSource === values.dataSource,
      );
      if (existingRoute) {
        await updateRoute.mutateAsync({
          request: {
            id: existingRoute.id,
            gramProject: state.project.slug,
            updateRouteRequestBody: {
              dataSource: values.dataSource,
              enabled: values.enabled,
              otelDestinationId: destinationID,
            },
          },
        });
      } else {
        await createRoute.mutateAsync({
          request: {
            gramProject: state.project.slug,
            createDataExportRouteForm: {
              dataSource: values.dataSource,
              enabled: values.enabled,
              otelDestinationId: destinationID,
            },
          },
        });
      }

      await invalidateProjectExports(state.project.slug);
      toast.success(existingRoute ? "Export updated" : "Export configured");
      setConfigureTarget(undefined);
    } catch (error) {
      await invalidateProjectExports(state.project.slug);
      toast.error(`Failed to configure export: ${toError(error).message}`);
    }
  };
  const handleSaveDestination = async (
    values: ConfigureDestinationValues,
  ): Promise<void> => {
    if (!destinationEditCandidate) return;

    const { project, destination } = destinationEditCandidate;
    try {
      await updateDestination.mutateAsync({
        request: {
          id: destination.id,
          gramProject: project.slug,
          updateDestinationRequestBody: {
            destinationType: "otel",
            name: values.destinationName.trim(),
            sensitiveData: values.includeSensitiveData ? "include" : "exclude",
            otel: {
              endpointUrl: values.endpointUrl.trim(),
              headers: values.headers.map(writeOnlyHeaderInput),
            },
          },
        },
      });
      await invalidateProjectExports(project.slug);
      toast.success("Destination updated");
      setDestinationEditCandidate(undefined);
    } catch (error) {
      await invalidateProjectExports(project.slug);
      toast.error(`Failed to update destination: ${toError(error).message}`);
    }
  };

  const handleToggleExport = async (
    project: ProjectEntry,
    route: DataExportRoute,
    enabled: boolean,
  ) => {
    const queryKey = aggregateQueryKey;
    await queryClient.cancelQueries({ queryKey });
    const previous =
      queryClient.getQueryData<ListDataExportsForOrgResult>(queryKey);
    queryClient.setQueryData<ListDataExportsForOrgResult>(
      queryKey,
      (current) =>
        current
          ? {
              ...current,
              routes: current.routes.map((candidate) =>
                candidate.id === route.id
                  ? { ...candidate, enabled }
                  : candidate,
              ),
            }
          : current,
    );

    try {
      await updateRoute.mutateAsync({
        request: {
          id: route.id,
          gramProject: project.slug,
          updateRouteRequestBody: {
            dataSource: route.dataSource,
            enabled,
            otelDestinationId: route.otelDestinationId,
          },
        },
      });
    } catch (error) {
      queryClient.setQueryData(queryKey, previous);
      toast.error(`Failed to update export: ${toError(error).message}`);
    } finally {
      await Promise.all([
        invalidateDataExportRoutes(queryClient, [
          { gramProject: project.slug },
        ]),
        queryClient.invalidateQueries({ queryKey: aggregateQueryKey }),
      ]);
    }
  };

  const handleDeleteExport = async () => {
    if (!deleteCandidate) return;
    try {
      await deleteRoute.mutateAsync({
        request: {
          id: deleteCandidate.route.id,
          gramProject: deleteCandidate.project.slug,
        },
      });
      toast.success("Export deleted");
      setDeleteCandidate(undefined);
    } catch (error) {
      toast.error(`Failed to delete export: ${toError(error).message}`);
    } finally {
      await invalidateProjectExports(deleteCandidate.project.slug);
    }
  };

  const newExportProject = availableProjectExports[0]?.project;
  const newExportAction = newExportProject ? (
    <RequireScope scope="org:admin" level="component">
      <Button
        variant="primary"
        size="sm"
        onClick={() =>
          setConfigureTarget({ projectSlug: newExportProject.slug })
        }
      >
        New export
      </Button>
    </RequireScope>
  ) : null;

  let pageContent: JSX.Element | null;
  if (projectsQuery.isPending || projectQueriesPending) {
    pageContent = <SkeletonTable />;
  } else if (projects.length === 0) {
    pageContent = (
      <InlineEmptyState
        icon="folder"
        heading="No projects yet"
        description="Create a project before configuring an export."
      />
    );
  } else if (configuredExports.length > 0) {
    pageContent = (
      <>
        <ExportAnimationStyles />
        <ExportMap
          exports={configuredExports}
          mutating={mutating}
          onConfigure={(project, route) =>
            setConfigureTarget({
              projectSlug: project.slug,
              routeId: route.id,
            })
          }
          onConfigureDestination={(project, destination) =>
            setDestinationEditCandidate({ project, destination })
          }
          onToggle={(project, route, enabled) =>
            void handleToggleExport(project, route, enabled)
          }
          onDelete={(project, route) => setDeleteCandidate({ project, route })}
        />
      </>
    );
  } else if (projectQueryError) {
    pageContent = null;
  } else {
    pageContent = (
      <InlineEmptyState
        icon="send"
        heading="No exports configured"
        description="Create an export to choose what project data should be sent and where it should go."
        action={newExportAction}
      />
    );
  }

  return (
    <SettingsPage
      title="Data exports"
      description="Send project data to collectors you control. Each export connects one class of data to one configured endpoint."
      area="Data"
      primaryAction={newExportAction}
    >
      {projectsQuery.error ? (
        <Alert variant="error">
          Unable to load projects: {toError(projectsQuery.error).message}
        </Alert>
      ) : null}
      {projectQueryError ? (
        <Alert variant="error">
          Some project exports could not be loaded:{" "}
          {toError(projectQueryError).message}
        </Alert>
      ) : null}

      {pageContent}

      {configureState ? (
        <ConfigureExportSheet
          key={`${configureState.project.slug}:${configureRoute?.id ?? "new"}:${configureState.pending ? "pending" : "ready"}`}
          projects={configureProjects}
          project={configureState.project}
          dataSources={configureDataSources}
          destinations={configureState.destinations}
          route={configureRoute}
          loading={configureState.pending}
          saving={
            createDestination.isPending ||
            createRoute.isPending ||
            updateRoute.isPending
          }
          onClose={() => setConfigureTarget(undefined)}
          onProjectChange={(projectSlug) => setConfigureTarget({ projectSlug })}
          onSave={handleSaveExport}
        />
      ) : null}
      {destinationEditCandidate ? (
        <ConfigureDestinationSheet
          key={destinationEditCandidate.destination.id}
          destination={destinationEditCandidate.destination}
          saving={updateDestination.isPending}
          onClose={() => setDestinationEditCandidate(undefined)}
          onSave={handleSaveDestination}
        />
      ) : null}

      <DeleteExportDialog
        exportRoute={deleteCandidate?.route}
        deleting={deleteRoute.isPending}
        onOpenChange={(open) => {
          if (!open) setDeleteCandidate(undefined);
        }}
        onConfirm={() => void handleDeleteExport()}
      />
    </SettingsPage>
  );
}

function ExportAnimationStyles(): JSX.Element {
  return (
    <style>{`
      @keyframes data-export-flow {
        to { stroke-dashoffset: -30; }
      }

      .data-export-flow-line {
        animation: data-export-flow 1.1s linear infinite;
      }

      @media (prefers-reduced-motion: reduce) {
        .data-export-flow-line {
          animation: none;
          stroke-dasharray: none;
        }
      }
    `}</style>
  );
}

type ExportDestinationGroup = {
  destination: VisualDestination;
  firstExport: ProjectExportRow;
  exports: ProjectExportRow[];
};

const EXPORT_NODE_HEIGHT = 112;
const EXPORT_ROW_GAP = 24;

function groupExportsByDestination(
  exports: ProjectExportRow[],
): ExportDestinationGroup[] {
  const groups = new Map<string, ExportDestinationGroup>();

  for (const exportRow of exports) {
    const destination = visualDestination(exportRow);
    const existing = groups.get(destination.key);
    if (existing) {
      existing.exports.push(exportRow);
      continue;
    }

    groups.set(destination.key, {
      destination,
      firstExport: exportRow,
      exports: [exportRow],
    });
  }

  return [...groups.values()];
}

type ExportMapProps = {
  exports: ProjectExportRow[];
  mutating: boolean;
  onConfigure: (project: ProjectEntry, route: DataExportRoute) => void;
  onConfigureDestination: (
    project: ProjectEntry,
    destination: OtelDataExportDestination,
  ) => void;
  onToggle: (
    project: ProjectEntry,
    route: DataExportRoute,
    enabled: boolean,
  ) => void;
  onDelete: (project: ProjectEntry, route: DataExportRoute) => void;
};

export function ExportMap({
  exports,
  mutating,
  onConfigure,
  onConfigureDestination,
  onToggle,
  onDelete,
}: ExportMapProps): JSX.Element {
  const markerID = `export-map-arrow-${useId().replaceAll(":", "")}`;
  const destinationGroups = groupExportsByDestination(exports);

  return (
    <div className="overflow-x-auto border bg-card px-6 py-6">
      <div className="min-w-[960px]">
        <div className="grid grid-cols-[360px_minmax(180px,260px)_minmax(360px,1fr)] items-end pb-2">
          <span className="text-eyebrow text-muted-foreground">Data</span>
          <span aria-hidden="true" />
          <span className="text-eyebrow text-muted-foreground">Sent to</span>
        </div>
        <div className="space-y-6">
          {destinationGroups.map((group, groupIndex) => (
            <div
              key={group.destination.key}
              className="grid grid-cols-[360px_minmax(180px,260px)_minmax(360px,1fr)] gap-y-6"
              style={{
                gridTemplateRows: `repeat(${group.exports.length}, ${EXPORT_NODE_HEIGHT}px)`,
              }}
            >
              {group.exports.map((exportRow, rowIndex) => (
                <ExportSourceNode
                  key={exportRow.route.id}
                  exportRow={exportRow}
                  row={rowIndex + 1}
                  mutating={mutating}
                  onConfigure={onConfigure}
                  onToggle={onToggle}
                  onDelete={onDelete}
                />
              ))}
              <ExportFlow
                exports={group.exports}
                markerID={`${markerID}-${groupIndex}`}
              />
              <ExportDestinationNode
                destination={group.destination}
                configuredDestination={group.firstExport.destination}
                project={group.firstExport.project}
                rowSpan={group.exports.length}
                mutating={mutating}
                onConfigure={onConfigureDestination}
              />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function ExportSourceNode({
  exportRow,
  row,
  mutating,
  onConfigure,
  onToggle,
  onDelete,
}: {
  exportRow: ProjectExportRow;
  row: number;
  mutating: boolean;
  onConfigure: ExportMapProps["onConfigure"];
  onToggle: ExportMapProps["onToggle"];
  onDelete: ExportMapProps["onDelete"];
}): JSX.Element {
  const { project, route } = exportRow;
  const source = visualSource(exportRow);

  return (
    <div
      className="relative h-full border border-foreground px-5 py-3"
      style={{ gridColumn: 1, gridRow: row }}
    >
      <RequireScope scope="org:admin" level="component">
        <button
          type="button"
          onClick={() => onConfigure(project, route)}
          disabled={mutating}
          aria-label={`Configure ${source.name} export`}
          className="absolute inset-0 z-10 cursor-pointer transition-colors hover:bg-muted/30 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-foreground focus-visible:outline-none disabled:cursor-not-allowed disabled:hover:bg-transparent"
        />
      </RequireScope>
      <div className="pointer-events-none relative z-20 min-w-0 pr-10 pb-8">
        <div className="mb-1 flex items-center gap-2">
          <ProjectAvatar project={project} className="size-3 min-h-3 min-w-3" />
          <span className="text-eyebrow text-muted-foreground">
            {project.name}
          </span>
        </div>
        <Text className="truncate font-medium">{source.name}</Text>
        <span className="mt-1 block truncate font-mono text-xs text-placeholder">
          {source.detail}
        </span>
      </div>
      <RequireScope scope="org:admin" level="component">
        <div className="pointer-events-auto absolute top-3 right-3 z-30">
          <MoreActions
            actions={[
              {
                label: "Configure export",
                icon: "pencil",
                onClick: () => onConfigure(project, route),
              },
              {
                label: "Delete export",
                icon: "trash-2",
                destructive: true,
                disabled: mutating,
                onClick: () => onDelete(project, route),
              },
            ]}
          />
        </div>
      </RequireScope>
      <div className="pointer-events-none absolute right-3 bottom-3 z-20 flex items-center gap-2">
        <span
          className={
            route.enabled
              ? "flex items-center gap-1.5 text-sm text-default-success"
              : "text-sm text-placeholder"
          }
        >
          {route.enabled ? <Icon name="check" className="size-3.5" /> : null}
          {route.enabled ? "Enabled" : "Paused"}
        </span>
        <RequireScope scope="org:admin" level="component">
          <div className="pointer-events-auto">
            <Switch
              checked={route.enabled}
              onCheckedChange={(enabled) => onToggle(project, route, enabled)}
              disabled={mutating}
              aria-label={`${route.enabled ? "Pause" : "Enable"} export from ${source.name}`}
            />
          </div>
        </RequireScope>
      </div>
    </div>
  );
}

function ExportFlow({
  exports,
  markerID,
}: {
  exports: ProjectExportRow[];
  markerID: string;
}): JSX.Element {
  const height =
    exports.length * EXPORT_NODE_HEIGHT + (exports.length - 1) * EXPORT_ROW_GAP;
  const destinationY = height / 2;
  const anyEnabled = exports.some(({ route }) => route.enabled);

  return (
    <svg
      viewBox={`0 0 200 ${height}`}
      preserveAspectRatio="none"
      className="h-full w-full overflow-visible"
      style={{ gridColumn: 2, gridRow: `1 / span ${exports.length}` }}
      aria-hidden="true"
    >
      <defs>
        <marker
          id={markerID}
          markerWidth="8"
          markerHeight="8"
          refX="7"
          refY="4"
          orient="auto"
        >
          <path
            d="M0,0 L8,4 L0,8 Z"
            className="fill-card stroke-card"
            strokeWidth="3"
            strokeLinejoin="round"
          />
          <path
            d="M0,0 L8,4 L0,8 Z"
            className={anyEnabled ? "fill-foreground" : "fill-muted-foreground"}
          />
        </marker>
      </defs>
      {exports.map(({ route }, rowIndex) => {
        const sourceY =
          rowIndex * (EXPORT_NODE_HEIGHT + EXPORT_ROW_GAP) +
          EXPORT_NODE_HEIGHT / 2;

        return (
          <path
            key={route.id}
            d={`M 0 ${sourceY} C 70 ${sourceY}, 120 ${destinationY}, 184 ${destinationY}`}
            fill="none"
            className={
              route.enabled
                ? "data-export-flow-line stroke-foreground"
                : "stroke-muted-foreground"
            }
            strokeWidth="2"
            strokeLinecap="round"
            strokeDasharray={route.enabled ? "8 7" : undefined}
            vectorEffect="non-scaling-stroke"
          />
        );
      })}
      <path
        d={`M 184 ${destinationY} L 196 ${destinationY}`}
        fill="none"
        className={anyEnabled ? "stroke-foreground" : "stroke-muted-foreground"}
        strokeWidth="2"
        strokeLinecap="round"
        vectorEffect="non-scaling-stroke"
        markerEnd={`url(#${markerID})`}
      />
    </svg>
  );
}

function ExportDestinationNode({
  destination,
  configuredDestination,
  project,
  rowSpan,
  mutating,
  onConfigure,
}: {
  destination: VisualDestination;
  configuredDestination: OtelDataExportDestination | undefined;
  project: ProjectEntry;
  rowSpan: number;
  mutating: boolean;
  onConfigure: ExportMapProps["onConfigureDestination"];
}): JSX.Element {
  return (
    <div
      className="relative flex min-h-24 self-center items-center gap-4 border px-5 py-3"
      style={{ gridColumn: 3, gridRow: `1 / span ${rowSpan}` }}
    >
      {configuredDestination ? (
        <RequireScope scope="org:admin" level="component">
          <button
            type="button"
            onClick={() => onConfigure(project, configuredDestination)}
            disabled={mutating}
            aria-label={`Configure ${destination.name} destination`}
            className="absolute inset-0 z-10 cursor-pointer transition-colors hover:bg-muted/30 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-foreground focus-visible:outline-none disabled:cursor-not-allowed disabled:hover:bg-transparent"
          />
        </RequireScope>
      ) : null}
      <div className="pointer-events-none relative z-20 min-w-0 pr-10">
        <div className="flex items-center gap-2">
          <Badge size="sm">{destination.type}</Badge>
          <Text className="truncate font-medium">{destination.name}</Text>
          {destination.sensitive ? (
            <Badge variant="warning" background={false} size="sm">
              Sensitive
            </Badge>
          ) : null}
        </div>
        <span className="mt-1 block truncate font-mono text-xs text-placeholder">
          {destination.detail}
        </span>
      </div>
      {configuredDestination ? (
        <RequireScope scope="org:admin" level="component">
          <div className="pointer-events-auto absolute top-3 right-3 z-30">
            <MoreActions
              actions={[
                {
                  label: "Configure destination",
                  icon: "pencil",
                  disabled: mutating,
                  onClick: () => onConfigure(project, configuredDestination),
                },
              ]}
            />
          </div>
        </RequireScope>
      ) : null}
    </div>
  );
}

function DeleteExportDialog({
  exportRoute,
  deleting,
  onOpenChange,
  onConfirm,
}: {
  exportRoute: DataExportRoute | undefined;
  deleting: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}): JSX.Element {
  return (
    <Dialog open={exportRoute !== undefined} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete export?</Dialog.Title>
          <Dialog.Description>
            {exportRoute
              ? `${sourceLabel(exportRoute.dataSource)} will stop exporting from this project.`
              : "This export will be removed."}
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={deleting}
          >
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            size="sm"
            onClick={onConfirm}
            disabled={deleting}
          >
            {deleting ? "Deleting" : "Delete export"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
