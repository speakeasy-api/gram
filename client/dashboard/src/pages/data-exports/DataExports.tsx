import { InlineEmptyState } from "@/components/inline-empty-state";
import { SettingsPage, SettingsSection } from "@/components/page-templates";
import { ProjectAvatar } from "@/components/project-menu";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { MoreActions } from "@/components/ui/MoreActions";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { toError } from "@/lib/errors";
import { useQueryClient } from "@tanstack/react-query";
import { DataSource } from "@gram/client/models/components/createdataexportrouteform.js";
import type { DataExportRoute } from "@gram/client/models/components/dataexportroute.js";
import type { ListDataExportRoutesResult } from "@gram/client/models/components/listdataexportroutesresult.js";
import type { OtelDestination } from "@gram/client/models/components/oteldestination.js";
import type { ProjectEntry } from "@gram/client/models/components/projectentry.js";
import { useCreateDataExportRouteMutation } from "@gram/client/react-query/createDataExportRoute.js";
import { useCreateOtelDestinationMutation } from "@gram/client/react-query/createOtelDestination.js";
import {
  invalidateDataExportRoutes,
  queryKeyDataExportRoutes,
  useDataExportRoutes,
} from "@gram/client/react-query/dataExportRoutes.js";
import { useDeleteDataExportRouteMutation } from "@gram/client/react-query/deleteDataExportRoute.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import {
  invalidateOtelDestinations,
  useOtelDestinations,
} from "@gram/client/react-query/otelDestinations.js";
import { useUpdateDataExportRouteMutation } from "@gram/client/react-query/updateDataExportRoute.js";
import { Plus } from "lucide-react";
import { type ReactNode, useId, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  ConfigureExportSheet,
  type ConfigureExportValues,
} from "./ConfigureExportSheet";

const EMPTY_PROJECTS: ProjectEntry[] = [];
const EMPTY_DESTINATIONS: OtelDestination[] = [];
const EMPTY_ROUTES: DataExportRoute[] = [];

type ExportRow = {
  route: DataExportRoute;
  destination?: OtelDestination;
};

type ProjectOption = DropdownItem & { project: ProjectEntry };

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
  const queryClient = useQueryClient();
  const projectsQuery = useListProjects({ organizationId: organization.id });
  const projects = projectsQuery.data?.projects ?? EMPTY_PROJECTS;
  const [selectedProjectSlug, setSelectedProjectSlug] = useState<string>();
  const selectedProject =
    projects.find((project) => project.slug === selectedProjectSlug) ??
    projects[0];
  const gramProject = selectedProject?.slug ?? "";
  const destinationsQuery = useOtelDestinations({ gramProject }, undefined, {
    enabled: gramProject !== "",
  });
  const routesQuery = useDataExportRoutes({ gramProject }, undefined, {
    enabled: gramProject !== "",
  });
  const createDestination = useCreateOtelDestinationMutation();
  const createRoute = useCreateDataExportRouteMutation();
  const updateRoute = useUpdateDataExportRouteMutation();
  const deleteRoute = useDeleteDataExportRouteMutation();
  const [configureOpen, setConfigureOpen] = useState(false);
  const [deleteCandidate, setDeleteCandidate] = useState<DataExportRoute>();

  const destinations =
    destinationsQuery.data?.destinations ?? EMPTY_DESTINATIONS;
  const routes = routesQuery.data?.routes ?? EMPTY_ROUTES;
  const destinationByID = useMemo(
    () =>
      new Map(destinations.map((destination) => [destination.id, destination])),
    [destinations],
  );
  const exports = useMemo<ExportRow[]>(
    () =>
      routes.map((route) => ({
        route,
        destination: route.otelDestinationId
          ? destinationByID.get(route.otelDestinationId)
          : undefined,
      })),
    [destinationByID, routes],
  );
  const selectedSourceRoute = routes.find(
    (route) => route.dataSource === DataSource.ProductTelemetry,
  );

  const invalidateProjectExports = async (projectSlug: string) =>
    Promise.all([
      invalidateOtelDestinations(queryClient, [{ gramProject: projectSlug }]),
      invalidateDataExportRoutes(queryClient, [{ gramProject: projectSlug }]),
    ]);

  const handleSaveExport = async (values: ConfigureExportValues) => {
    const project = projects.find(
      (candidate) => candidate.slug === values.projectSlug,
    );
    if (!project) return;

    try {
      const existingDestination = destinations.find(
        (destination) => destination.id === values.destinationId,
      );
      let destinationID = existingDestination?.id;
      if (!destinationID) {
        const destination = await createDestination.mutateAsync({
          request: {
            gramProject: project.slug,
            createOtelDestinationForm: {
              name: values.destinationName.trim(),
              endpointUrl: values.endpointUrl.trim(),
              sensitiveData: values.includeSensitiveData
                ? "include"
                : "exclude",
              headers: values.headers.map((header) => ({
                name: header.name.trim(),
                value: header.value,
              })),
            },
          },
        });
        destinationID = destination.id;
      }

      const existingRoute = routes.find(
        (route) => route.dataSource === values.dataSource,
      );
      if (existingRoute) {
        await updateRoute.mutateAsync({
          request: {
            id: existingRoute.id,
            gramProject: project.slug,
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
            gramProject: project.slug,
            createDataExportRouteForm: {
              dataSource: values.dataSource,
              enabled: values.enabled,
              otelDestinationId: destinationID,
            },
          },
        });
      }

      await invalidateProjectExports(project.slug);
      toast.success(existingRoute ? "Export updated" : "Export configured");
      setConfigureOpen(false);
    } catch (error) {
      await invalidateProjectExports(project.slug);
      toast.error(`Failed to configure export: ${toError(error).message}`);
    }
  };

  const handleToggleExport = async (
    route: DataExportRoute,
    enabled: boolean,
  ) => {
    if (!selectedProject) return;
    const queryKey = queryKeyDataExportRoutes({
      gramProject: selectedProject.slug,
    });
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
          gramProject: selectedProject.slug,
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
        { gramProject: selectedProject.slug },
      ]);
    }
  };

  const handleDeleteExport = async () => {
    if (!deleteCandidate || !selectedProject) return;
    try {
      await deleteRoute.mutateAsync({
        request: {
          id: deleteCandidate.id,
          gramProject: selectedProject.slug,
        },
      });
      await invalidateDataExportRoutes(queryClient, [
        { gramProject: selectedProject.slug },
      ]);
      toast.success("Export deleted");
      setDeleteCandidate(undefined);
    } catch (error) {
      toast.error(`Failed to delete export: ${toError(error).message}`);
    }
  };

  const projectOptions = useMemo<ProjectOption[]>(
    () =>
      projects.map((project) => ({
        value: project.slug,
        label: project.name,
        project,
        icon: (
          <ProjectAvatar project={project} className="size-4 min-h-4 min-w-4" />
        ),
      })),
    [projects],
  );
  const selectedProjectOption = projectOptions.find(
    (option) => option.value === selectedProject?.slug,
  );
  const loading =
    projectsQuery.isPending ||
    (selectedProject !== undefined &&
      (destinationsQuery.isPending || routesQuery.isPending));
  const error =
    projectsQuery.error ?? destinationsQuery.error ?? routesQuery.error;
  const mutating =
    createDestination.isPending ||
    createRoute.isPending ||
    updateRoute.isPending ||
    deleteRoute.isPending;

  const primaryAction = selectedProject ? (
    <div className="flex items-center gap-2">
      <Combobox
        items={projectOptions}
        selected={selectedProjectOption}
        onSelectionChange={(option) =>
          setSelectedProjectSlug(option.project.slug)
        }
        variant="secondary"
        className="h-9 min-w-52"
        contentClassName="w-72"
        searchable
        searchPlaceholder="Search projects"
      >
        <div className="flex min-w-0 items-center gap-2">
          <ProjectAvatar
            project={selectedProject}
            className="size-4 min-h-4 min-w-4"
          />
          <span className="truncate">{selectedProject.name}</span>
        </div>
      </Combobox>
      <RequireScope
        scope="project:write"
        resourceId={selectedProject.id}
        level="component"
      >
        <Button
          variant="primary"
          size="sm"
          onClick={() => setConfigureOpen(true)}
        >
          <Button.LeftIcon>
            <Plus className="size-3.5" />
          </Button.LeftIcon>
          Configure export
        </Button>
      </RequireScope>
    </div>
  ) : null;

  let pageContent: ReactNode;
  if (loading) {
    pageContent = <SkeletonTable />;
  } else if (projects.length === 0 || !selectedProject) {
    pageContent = (
      <InlineEmptyState
        icon="folder"
        heading="No projects yet"
        description="Create a project before configuring an export."
      />
    );
  } else if (exports.length === 0) {
    pageContent = (
      <InlineEmptyState
        icon="send"
        heading="No exports configured"
        description={`Choose what ${selectedProject.name} should send and where it should go.`}
        action={primaryAction}
      />
    );
  } else {
    pageContent = (
      <>
        <ExportAnimationStyles />
        <ExportMap exports={exports} />
        <SettingsSection>
          <SettingsSection.Header>
            <SettingsSection.Title>Configured exports</SettingsSection.Title>
            <SettingsSection.Description>
              Data leaving {selectedProject.name}.
            </SettingsSection.Description>
          </SettingsSection.Header>
          <StackExports
            exports={exports}
            project={selectedProject}
            mutating={mutating}
            onConfigure={() => setConfigureOpen(true)}
            onToggle={(route, enabled) =>
              void handleToggleExport(route, enabled)
            }
            onDelete={setDeleteCandidate}
          />
        </SettingsSection>
      </>
    );
  }

  return (
    <SettingsPage
      title="Data exports"
      description="Send project data to collectors you control. Each export connects one class of data to one configured endpoint."
      area="Data"
      primaryAction={primaryAction}
    >
      {error ? (
        <Alert variant="error">
          Unable to load data exports: {toError(error).message}
        </Alert>
      ) : null}

      {pageContent}

      {configureOpen && selectedProject ? (
        <ConfigureExportSheet
          key={selectedProject.slug}
          projects={projects}
          project={selectedProject}
          destinations={destinations}
          route={selectedSourceRoute}
          loading={destinationsQuery.isPending || routesQuery.isPending}
          saving={
            createDestination.isPending ||
            createRoute.isPending ||
            updateRoute.isPending
          }
          onClose={() => setConfigureOpen(false)}
          onProjectChange={setSelectedProjectSlug}
          onSave={handleSaveExport}
        />
      ) : null}

      <DeleteExportDialog
        exportRoute={deleteCandidate}
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
      @keyframes data-export-beam {
        to { stroke-dashoffset: -164; }
      }

      .data-export-beam {
        animation: data-export-beam 1.8s linear infinite;
        filter: drop-shadow(0 0 2px var(--stroke-success-default));
      }

      @media (prefers-reduced-motion: reduce) {
        .data-export-beam { display: none; }
      }
    `}</style>
  );
}

function ExportMap({ exports }: { exports: ExportRow[] }): JSX.Element {
  const markerID = `export-map-arrow-${useId().replaceAll(":", "")}`;

  return (
    <div className="overflow-x-auto border bg-card px-6 py-6">
      <div className="min-w-[860px]">
        <div className="grid grid-cols-[280px_minmax(160px,240px)_minmax(360px,1fr)] items-end pb-2">
          <span className="text-eyebrow text-muted-foreground">Data</span>
          <span aria-hidden="true" />
          <span className="text-eyebrow text-muted-foreground">Sent to</span>
        </div>
        <div className="space-y-3">
          {exports.map(({ route, destination }, index) => {
            const arrowID = `${markerID}-${index}`;
            const path = "M 0 32 C 70 32, 120 32, 196 32";
            return (
              <div
                key={route.id}
                className="grid grid-cols-[280px_minmax(160px,240px)_minmax(360px,1fr)] items-stretch"
              >
                <div className="flex min-h-20 flex-col justify-center border border-foreground px-5 py-4">
                  <Text className="font-medium">
                    {sourceLabel(route.dataSource)}
                  </Text>
                  <span className="mt-1 block font-mono text-xs text-placeholder">
                    OTLP traces &amp; logs
                  </span>
                </div>
                <svg
                  viewBox="0 0 200 64"
                  preserveAspectRatio="none"
                  className="h-full min-h-20 w-full overflow-visible"
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
                  <path
                    d={path}
                    fill="none"
                    className={
                      route.enabled
                        ? "stroke-foreground"
                        : "stroke-muted-foreground"
                    }
                    strokeWidth="2"
                    strokeDasharray={route.enabled ? undefined : "5 5"}
                    vectorEffect="non-scaling-stroke"
                    markerEnd={`url(#${arrowID})`}
                  />
                  {route.enabled ? (
                    <path
                      d={path}
                      fill="none"
                      className="data-export-beam stroke-success-highlight"
                      strokeWidth="3.5"
                      strokeLinecap="round"
                      strokeDasharray="24 140"
                      vectorEffect="non-scaling-stroke"
                    />
                  ) : null}
                </svg>
                <div className="flex min-h-20 items-center justify-between gap-4 border px-5 py-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <Text className="truncate font-medium">
                        {destination?.name ?? "Not configured"}
                      </Text>
                      {destination?.sensitiveData === "include" ? (
                        <Badge variant="warning" background={false} size="sm">
                          Sensitive
                        </Badge>
                      ) : null}
                    </div>
                    <span className="mt-1 block truncate font-mono text-xs text-placeholder">
                      {destination?.endpointUrl ??
                        "Configure this export to continue"}
                    </span>
                  </div>
                  <span
                    className={
                      route.enabled
                        ? "flex shrink-0 items-center gap-1.5 text-sm text-default-success"
                        : "shrink-0 text-sm text-placeholder"
                    }
                  >
                    {route.enabled ? (
                      <Icon name="check" className="size-3.5" />
                    ) : null}
                    {route.enabled ? "Enabled" : "Paused"}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function StackExports({
  exports,
  project,
  mutating,
  onConfigure,
  onToggle,
  onDelete,
}: {
  exports: ExportRow[];
  project: ProjectEntry;
  mutating: boolean;
  onConfigure: () => void;
  onToggle: (route: DataExportRoute, enabled: boolean) => void;
  onDelete: (route: DataExportRoute) => void;
}): JSX.Element {
  return (
    <div className="space-y-3">
      {exports.map(({ route, destination }) => (
        <ExportConnection
          key={route.id}
          route={route}
          destination={destination}
          project={project}
          mutating={mutating}
          onConfigure={onConfigure}
          onToggle={onToggle}
          onDelete={onDelete}
        />
      ))}
    </div>
  );
}

function ExportConnection({
  route,
  destination,
  project,
  mutating,
  onConfigure,
  onToggle,
  onDelete,
}: {
  route: DataExportRoute;
  destination?: OtelDestination;
  project: ProjectEntry;
  mutating: boolean;
  onConfigure: () => void;
  onToggle: (route: DataExportRoute, enabled: boolean) => void;
  onDelete: (route: DataExportRoute) => void;
}): JSX.Element {
  const label = sourceLabel(route.dataSource);

  return (
    <div className="overflow-x-auto border bg-card">
      <div className="grid min-w-[680px] grid-cols-[minmax(200px,0.8fr)_minmax(320px,1.2fr)_auto] items-center gap-5 px-5 py-4">
        <div className="min-w-0">
          <span className="text-eyebrow text-muted-foreground">
            {project.name}
          </span>
          <Text className="mt-1 truncate font-medium">{label}</Text>
        </div>

        <div className="flex min-w-0 items-center justify-between gap-4 border-l pl-5">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <Text className="truncate font-medium">
                {destination?.name ?? "Not configured"}
              </Text>
              {destination?.sensitiveData === "include" ? (
                <Badge variant="warning" background={false} size="sm">
                  Sensitive
                </Badge>
              ) : null}
            </div>
            <span className="mt-1 block truncate font-mono text-xs text-placeholder">
              {destination?.endpointUrl ?? "Configure this export to continue"}
            </span>
          </div>
          <span
            className={
              route.enabled
                ? "flex shrink-0 items-center gap-1.5 text-sm text-default-success"
                : "shrink-0 text-sm text-placeholder"
            }
          >
            {route.enabled ? <Icon name="check" className="size-3.5" /> : null}
            {route.enabled ? "Enabled" : "Paused"}
          </span>
        </div>

        <div className="flex items-center gap-2">
          <RequireScope
            scope="project:write"
            resourceId={project.id}
            level="component"
          >
            <Switch
              checked={route.enabled}
              onCheckedChange={(enabled) => onToggle(route, enabled)}
              disabled={mutating}
              aria-label={`${route.enabled ? "Pause" : "Enable"} export from ${label} to ${destination?.name ?? "the configured endpoint"}`}
            />
          </RequireScope>
          <RequireScope
            scope="project:write"
            resourceId={project.id}
            level="component"
          >
            <MoreActions
              actions={[
                {
                  label: "Configure export",
                  icon: "pencil",
                  onClick: onConfigure,
                },
                {
                  label: "Delete export",
                  icon: "trash-2",
                  destructive: true,
                  disabled: mutating,
                  onClick: () => onDelete(route),
                },
              ]}
            />
          </RequireScope>
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
