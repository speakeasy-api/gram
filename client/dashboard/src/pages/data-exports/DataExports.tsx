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
import { useQueries, useQueryClient } from "@tanstack/react-query";
import { DataSource } from "@gram/client/models/components/createdataexportrouteform.js";
import type { DataExportRoute } from "@gram/client/models/components/dataexportroute.js";
import type { ListDataExportRoutesResult } from "@gram/client/models/components/listdataexportroutesresult.js";
import type { ProjectEntry } from "@gram/client/models/components/projectentry.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useCreateDataExportRouteMutation } from "@gram/client/react-query/createDataExportRoute.js";
import { useCreateDataExportDestinationMutation } from "@gram/client/react-query/createDataExportDestination.js";
import {
  buildDataExportRoutesQuery,
  invalidateDataExportRoutes,
  queryKeyDataExportRoutes,
} from "@gram/client/react-query/dataExportRoutes.js";
import { useDeleteDataExportRouteMutation } from "@gram/client/react-query/deleteDataExportRoute.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import {
  buildDataExportDestinationsQuery,
  invalidateDataExportDestinations,
} from "@gram/client/react-query/dataExportDestinations.js";
import { useUpdateDataExportRouteMutation } from "@gram/client/react-query/updateDataExportRoute.js";
import { useId, useState } from "react";
import { toast } from "sonner";
import {
  ConfigureExportSheet,
  type ConfigureExportValues,
  type OtelDataExportDestination,
} from "./ConfigureExportSheet";

const EMPTY_PROJECTS: ProjectEntry[] = [];
const EMPTY_DESTINATIONS: OtelDataExportDestination[] = [];
const EMPTY_ROUTES: DataExportRoute[] = [];

type ExportRow = {
  route: DataExportRoute;
  destination?: OtelDataExportDestination;
};

type ProjectExportRow = ExportRow & {
  project: ProjectEntry;
};

type VisualDestination = {
  key: string;
  type: "OTEL" | "SIEM" | "S3";
  name: string;
  detail: string;
  sensitive: boolean;
  preview: boolean;
};

const TYPED_DESTINATION_PREVIEWS: VisualDestination[] = import.meta.env.DEV
  ? [
      {
        key: "siem-preview",
        type: "SIEM",
        name: "SIEM destination",
        detail: "Security events and findings",
        sensitive: false,
        preview: true,
      },
      {
        key: "s3-preview",
        type: "S3",
        name: "S3 archive",
        detail: "Long-term object storage",
        sensitive: false,
        preview: true,
      },
    ]
  : [];

function visualDestinations({
  route,
  destination,
}: ProjectExportRow): VisualDestination[] {
  return [
    {
      key: route.otelDestinationId ?? `${route.id}-otel`,
      type: "OTEL",
      name: destination?.name ?? "Not configured",
      detail:
        destination?.otel.endpointUrl ?? "Configure this export to continue",
      sensitive: destination?.sensitiveData === "include",
      preview: false,
    },
    ...TYPED_DESTINATION_PREVIEWS,
  ];
}

type VisualSource = {
  key: string;
  name: string;
  detail: string;
  preview: boolean;
};

type VisualConnection = {
  key: string;
  sourceIndex: number;
  destinationIndex: number;
};

const SOURCE_PREVIEWS: VisualSource[] = import.meta.env.DEV
  ? [
      {
        key: "risk-findings-preview",
        name: "Risk findings",
        detail: "Detected policy and security findings",
        preview: true,
      },
      {
        key: "agent-sessions-preview",
        name: "Agent sessions",
        detail: "Session lifecycle and execution events",
        preview: true,
      },
    ]
  : [];

function visualSources({ route }: ProjectExportRow): VisualSource[] {
  return [
    {
      key: route.id,
      name: sourceLabel(route.dataSource),
      detail: "OTLP traces & logs",
      preview: false,
    },
    ...SOURCE_PREVIEWS,
  ];
}

function visualConnections(
  sources: VisualSource[],
  destinations: VisualDestination[],
): VisualConnection[] {
  return [
    ...destinations.map((destination, destinationIndex) => ({
      key: `source-0-${destination.key}`,
      sourceIndex: 0,
      destinationIndex,
    })),
    ...sources.slice(1).map((source, index) => ({
      key: `${source.key}-destination-0`,
      sourceIndex: index + 1,
      destinationIndex: 0,
    })),
  ];
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

function sourceLabel(dataSource: string): string {
  if (dataSource === DataSource.ProductTelemetry) return "Product telemetry";
  return dataSource.replaceAll("_", " ");
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
  const projects = projectsQuery.data?.projects ?? EMPTY_PROJECTS;
  const routeQueries = useQueries({
    queries: projects.map((project) => ({
      ...buildDataExportRoutesQuery(client, { gramProject: project.slug }),
      throwOnError: false,
    })),
  });
  const destinationQueries = useQueries({
    queries: projects.map((project) => ({
      ...buildDataExportDestinationsQuery(client, {
        gramProject: project.slug,
      }),
      throwOnError: false,
    })),
  });
  const projectExports: ProjectExports[] = projects.map((project, index) => {
    const routeQuery = routeQueries[index];
    const destinationQuery = destinationQueries[index];
    const destinations = (
      destinationQuery?.data?.destinations ?? EMPTY_DESTINATIONS
    ).filter(
      (destination): destination is OtelDataExportDestination =>
        destination.destinationType === "otel" &&
        destination.otel !== undefined,
    );
    const routes = routeQuery?.data?.routes ?? EMPTY_ROUTES;
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
      pending:
        routeQuery?.isPending === true || destinationQuery?.isPending === true,
      error: routeQuery?.error ?? destinationQuery?.error,
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
      !state.routes.some(
        (route) => route.dataSource === DataSource.ProductTelemetry,
      ),
  );
  const projectQueriesPending = projectExports.some((state) => state.pending);
  const projectQueryError = projectExports.find((state) => state.error)?.error;

  const createDestination = useCreateDataExportDestinationMutation();
  const createRoute = useCreateDataExportRouteMutation();
  const updateRoute = useUpdateDataExportRouteMutation();
  const deleteRoute = useDeleteDataExportRouteMutation();
  const [configureProjectSlug, setConfigureProjectSlug] = useState<string>();
  const [deleteCandidate, setDeleteCandidate] = useState<DeleteCandidate>();
  const configureState = projectExports.find(
    ({ project }) => project.slug === configureProjectSlug,
  );
  const configureRoute = configureState?.routes.find(
    (route) => route.dataSource === DataSource.ProductTelemetry,
  );
  const configureProjects =
    configureState && configureRoute
      ? [configureState.project]
      : availableProjectExports.map(({ project }) => project);
  const mutating =
    createDestination.isPending ||
    createRoute.isPending ||
    updateRoute.isPending ||
    deleteRoute.isPending;

  const invalidateProjectExports = async (projectSlug: string) =>
    Promise.all([
      invalidateDataExportDestinations(queryClient, [
        { gramProject: projectSlug },
      ]),
      invalidateDataExportRoutes(queryClient, [{ gramProject: projectSlug }]),
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
      setConfigureProjectSlug(undefined);
    } catch (error) {
      await invalidateProjectExports(state.project.slug);
      toast.error(`Failed to configure export: ${toError(error).message}`);
    }
  };

  const handleToggleExport = async (
    project: ProjectEntry,
    route: DataExportRoute,
    enabled: boolean,
  ) => {
    const queryKey = queryKeyDataExportRoutes({ gramProject: project.slug });
    await queryClient.cancelQueries({ queryKey });
    const previous =
      queryClient.getQueryData<ListDataExportRoutesResult>(queryKey);
    queryClient.setQueryData<ListDataExportRoutesResult>(queryKey, (current) =>
      current
        ? {
            ...current,
            routes: current.routes.map((candidate) =>
              candidate.id === route.id ? { ...candidate, enabled } : candidate,
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
      await invalidateDataExportRoutes(queryClient, [
        { gramProject: project.slug },
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
      await invalidateDataExportRoutes(queryClient, [
        { gramProject: deleteCandidate.project.slug },
      ]);
      toast.success("Export deleted");
      setDeleteCandidate(undefined);
    } catch (error) {
      toast.error(`Failed to delete export: ${toError(error).message}`);
    }
  };

  const newExportProject = availableProjectExports[0]?.project;
  const newExportAction = newExportProject ? (
    <RequireScope scope="org:admin" level="component">
      <Button
        variant="primary"
        size="sm"
        onClick={() => setConfigureProjectSlug(newExportProject.slug)}
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
          onConfigure={(project) => setConfigureProjectSlug(project.slug)}
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
          key={`${configureState.project.slug}:${configureState.pending ? "pending" : "ready"}`}
          projects={configureProjects}
          project={configureState.project}
          destinations={configureState.destinations}
          route={configureRoute}
          loading={configureState.pending}
          saving={
            createDestination.isPending ||
            createRoute.isPending ||
            updateRoute.isPending
          }
          onClose={() => setConfigureProjectSlug(undefined)}
          onProjectChange={setConfigureProjectSlug}
          onSave={handleSaveExport}
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
        .data-export-flow-line { animation: none; }
      }
    `}</style>
  );
}

function ExportMap({
  exports,
  mutating,
  onConfigure,
  onToggle,
  onDelete,
}: {
  exports: ProjectExportRow[];
  mutating: boolean;
  onConfigure: (project: ProjectEntry) => void;
  onToggle: (
    project: ProjectEntry,
    route: DataExportRoute,
    enabled: boolean,
  ) => void;
  onDelete: (project: ProjectEntry, route: DataExportRoute) => void;
}): JSX.Element {
  const markerID = `export-map-arrow-${useId().replaceAll(":", "")}`;

  return (
    <div className="overflow-x-auto border bg-card px-6 py-6">
      <div className="min-w-[960px]">
        <div className="grid grid-cols-[360px_minmax(180px,260px)_minmax(360px,1fr)] items-end pb-2">
          <span className="text-eyebrow text-muted-foreground">Data</span>
          <span aria-hidden="true" />
          <span className="text-eyebrow text-muted-foreground">Sent to</span>
        </div>
        <div className="space-y-6">
          {exports.map((exportRow, exportIndex) => {
            const { project, route } = exportRow;
            const sources = visualSources(exportRow);
            const destinations = visualDestinations(exportRow);
            const connections = visualConnections(sources, destinations);
            const arrowID = `${markerID}-${exportIndex}`;
            return (
              <div
                key={route.id}
                className="grid grid-cols-[360px_minmax(180px,260px)_minmax(360px,1fr)] items-stretch"
              >
                <div className="space-y-3">
                  {sources.map((source, sourceIndex) => (
                    <div
                      key={source.key}
                      className={
                        source.preview
                          ? "flex min-h-24 items-center justify-between gap-4 border px-5 py-3"
                          : "flex min-h-24 items-center justify-between gap-4 border border-foreground px-5 py-3"
                      }
                    >
                      <div className="min-w-0">
                        {sourceIndex === 0 ? (
                          <div className="mb-1 flex items-center gap-2">
                            <ProjectAvatar
                              project={project}
                              className="size-3 min-h-3 min-w-3"
                            />
                            <span className="text-eyebrow text-muted-foreground">
                              {project.name}
                            </span>
                          </div>
                        ) : null}
                        <div className="flex items-center gap-2">
                          <Text className="truncate font-medium">
                            {source.name}
                          </Text>
                          {source.preview ? (
                            <Badge background={false} size="sm">
                              Preview
                            </Badge>
                          ) : null}
                        </div>
                        <span className="mt-1 block truncate font-mono text-xs text-placeholder">
                          {source.detail}
                        </span>
                      </div>
                      {sourceIndex === 0 ? (
                        <div className="flex shrink-0 items-center gap-2">
                          <span
                            className={
                              route.enabled
                                ? "flex items-center gap-1.5 text-sm text-default-success"
                                : "text-sm text-placeholder"
                            }
                          >
                            {route.enabled ? (
                              <Icon name="check" className="size-3.5" />
                            ) : null}
                            {route.enabled ? "Enabled" : "Paused"}
                          </span>
                          <RequireScope scope="org:admin" level="component">
                            <Switch
                              checked={route.enabled}
                              onCheckedChange={(enabled) =>
                                onToggle(project, route, enabled)
                              }
                              disabled={mutating}
                              aria-label={`${route.enabled ? "Pause" : "Enable"} export from ${source.name}`}
                            />
                          </RequireScope>
                          <RequireScope scope="org:admin" level="component">
                            <MoreActions
                              actions={[
                                {
                                  label: "Configure export",
                                  icon: "pencil",
                                  onClick: () => onConfigure(project),
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
                          </RequireScope>
                        </div>
                      ) : null}
                    </div>
                  ))}
                </div>
                <svg
                  viewBox="0 0 200 100"
                  preserveAspectRatio="none"
                  className="h-full min-h-24 w-full overflow-visible"
                  aria-hidden="true"
                >
                  <defs>
                    <marker
                      id={arrowID}
                      markerWidth="8"
                      markerHeight="8"
                      refX="7"
                      refY="4"
                      orient="auto"
                    >
                      <path
                        d="M0,0 L8,4 L0,8 Z"
                        className={
                          route.enabled
                            ? "fill-foreground"
                            : "fill-muted-foreground"
                        }
                      />
                    </marker>
                  </defs>
                  {connections.map((connection, connectionIndex) => {
                    const sourceY =
                      ((connection.sourceIndex + 0.5) / sources.length) * 100;
                    const destinationY =
                      ((connection.destinationIndex + 0.5) /
                        destinations.length) *
                      100;
                    const path = `M 0 ${sourceY} C 70 ${sourceY}, 120 ${destinationY}, 196 ${destinationY}`;
                    return (
                      <path
                        key={connection.key}
                        d={path}
                        fill="none"
                        className={
                          route.enabled
                            ? "data-export-flow-line stroke-foreground"
                            : "stroke-muted-foreground"
                        }
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeDasharray={route.enabled ? "8 7" : "5 6"}
                        vectorEffect="non-scaling-stroke"
                        markerEnd={`url(#${arrowID})`}
                        style={{
                          animationDelay: `-${connectionIndex * 0.2}s`,
                        }}
                      />
                    );
                  })}
                </svg>
                <div className="space-y-3">
                  {destinations.map((destination) => (
                    <div
                      key={destination.key}
                      className="flex min-h-24 items-center justify-between gap-4 border px-5 py-3"
                    >
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <Badge size="sm">{destination.type}</Badge>
                          <Text className="truncate font-medium">
                            {destination.name}
                          </Text>
                          {destination.preview ? (
                            <Badge background={false} size="sm">
                              Preview
                            </Badge>
                          ) : null}
                          {destination.sensitive ? (
                            <Badge
                              variant="warning"
                              background={false}
                              size="sm"
                            >
                              Sensitive
                            </Badge>
                          ) : null}
                        </div>
                        <span className="mt-1 block truncate font-mono text-xs text-placeholder">
                          {destination.detail}
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      </div>
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
