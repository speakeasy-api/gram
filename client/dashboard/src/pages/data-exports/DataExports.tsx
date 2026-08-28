import { InlineEmptyState } from "@/components/inline-empty-state";
import { SettingsPage, SettingsSection } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { MoreActions } from "@/components/ui/MoreActions";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { toError } from "@/lib/errors";
import { writeOnlyHeaderInput } from "@/lib/write-only-headers";
import { useQueryClient } from "@tanstack/react-query";
import { DataSource } from "@gram/client/models/components/createdataexportrouteform.js";
import type { DataExportRoute } from "@gram/client/models/components/dataexportroute.js";
import type { ListDataExportRoutesResult } from "@gram/client/models/components/listdataexportroutesresult.js";
import type { OtelDestination } from "@gram/client/models/components/oteldestination.js";
import { useCreateDataExportRouteMutation } from "@gram/client/react-query/createDataExportRoute.js";
import { useCreateOtelDestinationMutation } from "@gram/client/react-query/createOtelDestination.js";
import {
  invalidateDataExportRoutes,
  queryKeyDataExportRoutes,
  useDataExportRoutes,
} from "@gram/client/react-query/dataExportRoutes.js";
import { useDeleteDataExportRouteMutation } from "@gram/client/react-query/deleteDataExportRoute.js";
import { useDeleteOtelDestinationMutation } from "@gram/client/react-query/deleteOtelDestination.js";
import {
  invalidateOtelDestinations,
  useOtelDestinations,
} from "@gram/client/react-query/otelDestinations.js";
import { useUpdateDataExportRouteMutation } from "@gram/client/react-query/updateDataExportRoute.js";
import { useUpdateOtelDestinationMutation } from "@gram/client/react-query/updateOtelDestination.js";
import { Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import {
  DestinationEditorSheet,
  type DestinationFormValues,
} from "./DestinationEditorSheet";
import { RouteEditorSheet } from "./RouteEditorSheet";

const OTEL_FORWARDING_SOURCE = DataSource.OtelForwarding;
const EMPTY_DESTINATIONS: OtelDestination[] = [];
const EMPTY_ROUTES: DataExportRoute[] = [];

function SensitiveDataBadge({
  destination,
}: {
  destination: OtelDestination;
}): JSX.Element {
  if (destination.sensitiveData === "include") {
    return (
      <Badge variant="warning" background={false} size="md">
        Included
      </Badge>
    );
  }
  return <Badge size="md">Excluded</Badge>;
}

export default function DataExports(): JSX.Element {
  return (
    <RequireScope scope="project:read" level="page">
      <DataExportsInner />
    </RequireScope>
  );
}

function DataExportsInner(): JSX.Element {
  const queryClient = useQueryClient();
  const gramProject = useProjectSlugForRequests();
  const destinationsQuery = useOtelDestinations({ gramProject });
  const routesQuery = useDataExportRoutes({ gramProject });
  const createDestination = useCreateOtelDestinationMutation();
  const updateDestination = useUpdateOtelDestinationMutation();
  const deleteDestination = useDeleteOtelDestinationMutation();
  const createRoute = useCreateDataExportRouteMutation();
  const updateRoute = useUpdateDataExportRouteMutation();
  const deleteRoute = useDeleteDataExportRouteMutation();

  const [editor, setEditor] = useState<{
    destination?: OtelDestination;
    routeAfterCreateEnabled?: boolean;
  }>();
  const [deleteCandidate, setDeleteCandidate] = useState<OtelDestination>();
  const [routeEditorOpen, setRouteEditorOpen] = useState(false);

  const destinations =
    destinationsQuery.data?.destinations ?? EMPTY_DESTINATIONS;
  const routes = routesQuery.data?.routes ?? EMPTY_ROUTES;
  const destinationByID = useMemo(
    () =>
      new Map(destinations.map((destination) => [destination.id, destination])),
    [destinations],
  );
  const routedRows = useMemo(
    () =>
      routes.flatMap((route) => {
        if (
          route.dataSource !== OTEL_FORWARDING_SOURCE ||
          !route.otelDestinationId
        ) {
          return [];
        }
        const destination = destinationByID.get(route.otelDestinationId);
        return destination ? [{ route, destination }] : [];
      }),
    [destinationByID, routes],
  );
  const routedDestinationIDs = useMemo(
    () => new Set(routedRows.map(({ destination }) => destination.id)),
    [routedRows],
  );

  const handleSaveDestination = async (values: DestinationFormValues) => {
    try {
      let saved: OtelDestination;
      if (editor?.destination) {
        saved = await updateDestination.mutateAsync({
          request: {
            id: editor.destination.id,
            updateOtelDestinationRequestBody: {
              name: values.name.trim(),
              endpointUrl: values.endpointUrl.trim(),
              sensitiveData: values.includeSensitiveData
                ? "include"
                : "exclude",
              headers: values.headers.map(writeOnlyHeaderInput),
            },
          },
        });
        toast.success("Destination saved");
      } else {
        saved = await createDestination.mutateAsync({
          request: {
            createOtelDestinationForm: {
              name: values.name.trim(),
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
        toast.success("Destination created");
      }

      if (editor?.routeAfterCreateEnabled !== undefined) {
        try {
          await createRoute.mutateAsync({
            request: {
              createDataExportRouteForm: {
                dataSource: OTEL_FORWARDING_SOURCE,
                enabled: editor.routeAfterCreateEnabled,
                otelDestinationId: saved.id,
              },
            },
          });
        } catch (error) {
          toast.error(
            `Destination created, but routing failed: ${toError(error).message}`,
          );
        }
      }

      await Promise.all([
        invalidateOtelDestinations(queryClient, [{ gramProject }]),
        editor?.routeAfterCreateEnabled !== undefined
          ? invalidateDataExportRoutes(queryClient, [{ gramProject }])
          : Promise.resolve(),
      ]);
      setEditor(undefined);
    } catch (error) {
      toast.error(`Failed to save destination: ${toError(error).message}`);
    }
  };

  const handleDeleteDestination = async () => {
    if (!deleteCandidate) return;
    try {
      await deleteDestination.mutateAsync({
        request: { id: deleteCandidate.id },
      });
      await invalidateOtelDestinations(queryClient, [{ gramProject }]);
      toast.success("Destination deleted");
      setDeleteCandidate(undefined);
      setEditor(undefined);
    } catch (error) {
      toast.error(`Failed to delete destination: ${toError(error).message}`);
    }
  };

  const handleCreateRoute = async (
    destination: OtelDestination,
    enabled: boolean,
  ) => {
    try {
      await createRoute.mutateAsync({
        request: {
          createDataExportRouteForm: {
            dataSource: OTEL_FORWARDING_SOURCE,
            enabled,
            otelDestinationId: destination.id,
          },
        },
      });
      await invalidateDataExportRoutes(queryClient, [{ gramProject }]);
      toast.success(`Route to ${destination.name} created`);
      setRouteEditorOpen(false);
    } catch (error) {
      toast.error(`Failed to create route: ${toError(error).message}`);
    }
  };

  const handleToggleRoute = async (
    route: DataExportRoute,
    enabled: boolean,
  ) => {
    const queryKey = queryKeyDataExportRoutes({ gramProject });
    await queryClient.cancelQueries({ queryKey });
    const previous =
      queryClient.getQueryData<ListDataExportRoutesResult>(queryKey);
    queryClient.setQueryData<ListDataExportRoutesResult>(queryKey, (current) =>
      current
        ? {
            ...current,
            routes: current.routes.map((item) =>
              item.id === route.id ? { ...item, enabled } : item,
            ),
          }
        : current,
    );

    try {
      await updateRoute.mutateAsync({
        request: {
          id: route.id,
          updateRouteRequestBody: {
            dataSource: route.dataSource,
            enabled,
            otelDestinationId: route.otelDestinationId,
          },
        },
      });
    } catch (error) {
      queryClient.setQueryData(queryKey, previous);
      toast.error(`Failed to update route: ${toError(error).message}`);
    } finally {
      await invalidateDataExportRoutes(queryClient, [{ gramProject }]);
    }
  };

  const handleDeleteRoute = async (route: DataExportRoute) => {
    try {
      await deleteRoute.mutateAsync({ request: { id: route.id } });
      await invalidateDataExportRoutes(queryClient, [{ gramProject }]);
      toast.success("Destination removed from Product telemetry");
    } catch (error) {
      toast.error(`Failed to remove destination: ${toError(error).message}`);
    }
  };

  const loading = destinationsQuery.isPending || routesQuery.isPending;
  const error = destinationsQuery.error ?? routesQuery.error;
  const hasLoadedConfiguration =
    destinationsQuery.data !== undefined && routesQuery.data !== undefined;
  const mutating =
    createDestination.isPending ||
    updateDestination.isPending ||
    createRoute.isPending ||
    updateRoute.isPending ||
    deleteRoute.isPending;

  return (
    <SettingsPage
      title="Data exports"
      description={
        <span className="block max-w-[720px]">
          Send a copy of this project&apos;s data to collectors you own. Add a
          destination once, then route as many sources to it as you like. Header
          values are encrypted at rest and never returned by the API.{" "}
          <a
            href="https://docs.getgram.ai"
            target="_blank"
            rel="noopener noreferrer"
            className="text-link-primary underline underline-offset-2"
          >
            Read the docs
          </a>
        </span>
      }
      area="Observe"
    >
      {error && hasLoadedConfiguration ? (
        <Alert variant="error">
          Unable to refresh data exports: {toError(error).message}
        </Alert>
      ) : null}
      {error && !hasLoadedConfiguration ? (
        <Alert variant="error">
          Unable to load data exports: {toError(error).message}
        </Alert>
      ) : loading ? (
        <>
          <SkeletonTable />
          <SkeletonTable />
        </>
      ) : destinations.length === 0 ? (
        <InlineEmptyState
          icon="database"
          heading="No destinations yet"
          description="Add the OTLP endpoint of a collector you own. Once it exists you can route product telemetry to it."
          action={
            <div className="flex items-center gap-2">
              <RequireScope scope="project:write" level="component">
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() =>
                    setEditor({
                      destination: undefined,
                      routeAfterCreateEnabled: undefined,
                    })
                  }
                >
                  <Plus className="mr-1 size-3.5" />
                  New destination
                </Button>
              </RequireScope>
              <Button variant="secondary" size="sm" asChild>
                <a
                  href="https://docs.getgram.ai"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Read the docs
                </a>
              </Button>
            </div>
          }
        />
      ) : (
        <>
          <RouteDiagram rows={routedRows} />
          <RoutesSection
            rows={routedRows}
            mutating={mutating}
            onNewRoute={() => setRouteEditorOpen(true)}
            onEditDestination={(destination) =>
              setEditor({
                destination,
                routeAfterCreateEnabled: undefined,
              })
            }
            onToggleRoute={(route, enabled) =>
              void handleToggleRoute(route, enabled)
            }
            onDeleteRoute={(route) => void handleDeleteRoute(route)}
          />
          <DestinationsSection
            destinations={destinations}
            routes={routes}
            mutating={mutating}
            onNewDestination={() =>
              setEditor({
                destination: undefined,
                routeAfterCreateEnabled: undefined,
              })
            }
            onEditDestination={(destination) =>
              setEditor({
                destination,
                routeAfterCreateEnabled: undefined,
              })
            }
            onDeleteDestination={setDeleteCandidate}
          />
        </>
      )}

      {editor ? (
        <DestinationEditorSheet
          destination={editor.destination}
          routes={
            editor.destination
              ? routes.filter(
                  (route) => route.otelDestinationId === editor.destination?.id,
                )
              : []
          }
          saving={
            createDestination.isPending ||
            updateDestination.isPending ||
            createRoute.isPending
          }
          deleting={deleteDestination.isPending}
          onClose={() => setEditor(undefined)}
          onSave={handleSaveDestination}
          onRequestDelete={setDeleteCandidate}
        />
      ) : null}

      {routeEditorOpen ? (
        <RouteEditorSheet
          destinations={destinations}
          routedDestinationIDs={routedDestinationIDs}
          saving={createRoute.isPending}
          onClose={() => setRouteEditorOpen(false)}
          onCreate={handleCreateRoute}
          onCreateDestination={(enabled) => {
            setRouteEditorOpen(false);
            setEditor({
              destination: undefined,
              routeAfterCreateEnabled: enabled,
            });
          }}
        />
      ) : null}

      <DeleteDestinationDialog
        destination={deleteCandidate}
        deleting={deleteDestination.isPending}
        onOpenChange={(open) => {
          if (!open) setDeleteCandidate(undefined);
        }}
        onConfirm={() => void handleDeleteDestination()}
      />
    </SettingsPage>
  );
}

type SourceRouteRow = {
  route: DataExportRoute;
  destination: OtelDestination;
};

function RouteDiagram({ rows }: { rows: SourceRouteRow[] }): JSX.Element {
  return (
    <div className="overflow-x-auto border bg-card px-6 py-6">
      <style>{`
        @keyframes data-export-route-beam {
          to {
            stroke-dashoffset: -164;
          }
        }

        .data-export-route-beam {
          animation: data-export-route-beam 1.8s linear infinite;
          filter: drop-shadow(0 0 2px var(--stroke-success-default));
        }

        @media (prefers-reduced-motion: reduce) {
          .data-export-route-beam {
            display: none;
          }
        }
      `}</style>
      <div className="min-w-[900px]">
        <div className="grid grid-cols-[280px_minmax(140px,220px)_minmax(420px,1fr)] items-end gap-x-0 pb-2">
          <span className="text-eyebrow text-muted-foreground">Source</span>
          <span aria-hidden="true" />
          <span className="text-eyebrow text-muted-foreground">
            Destinations
          </span>
        </div>

        <div className="grid grid-cols-[280px_minmax(140px,220px)_minmax(420px,1fr)] gap-x-0">
          <div className="flex flex-col justify-center">
            <div className="border border-foreground px-5 py-4">
              <Text className="font-medium">Product telemetry</Text>
              <span className="mt-1 block font-mono text-xs text-placeholder">
                OTLP traces &amp; logs
              </span>
            </div>
            <Text muted className="mt-3 text-xs">
              More sources will appear as they ship.
            </Text>
          </div>

          <div className="relative min-h-full" aria-hidden="true">
            {rows.length > 0 ? (
              <svg
                viewBox="0 0 200 100"
                preserveAspectRatio="none"
                className="absolute inset-0 h-full w-full overflow-visible"
              >
                <defs>
                  <marker
                    id="route-arrow-active"
                    markerWidth="8"
                    markerHeight="8"
                    refX="7"
                    refY="4"
                    orient="auto"
                  >
                    <path d="M0,0 L8,4 L0,8 Z" className="fill-foreground" />
                  </marker>
                  <marker
                    id="route-arrow-paused"
                    markerWidth="8"
                    markerHeight="8"
                    refX="7"
                    refY="4"
                    orient="auto"
                  >
                    <path
                      d="M0,0 L8,4 L0,8 Z"
                      className="fill-muted-foreground"
                    />
                  </marker>
                </defs>
                {rows.map(({ route }, index) => {
                  const destinationY =
                    ((index + 0.5) / Math.max(rows.length, 1)) * 100;
                  const path = `M 0 50 C 75 50, 110 ${destinationY}, 196 ${destinationY}`;
                  return (
                    <g key={route.id}>
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
                        markerEnd={
                          route.enabled
                            ? "url(#route-arrow-active)"
                            : "url(#route-arrow-paused)"
                        }
                      />
                      {route.enabled ? (
                        <path
                          d={path}
                          fill="none"
                          className="data-export-route-beam stroke-success-highlight"
                          strokeWidth="3.5"
                          strokeLinecap="round"
                          strokeDasharray="24 140"
                          vectorEffect="non-scaling-stroke"
                        />
                      ) : null}
                    </g>
                  );
                })}
              </svg>
            ) : null}
          </div>

          <div className="space-y-3">
            {rows.length === 0 ? (
              <Text muted className="flex min-h-16 items-center border px-5">
                No destinations routed
              </Text>
            ) : (
              rows.map(({ route, destination }) => (
                <div
                  key={route.id}
                  className="flex min-h-16 items-center justify-between gap-4 border px-5 py-3"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <Text
                        className={
                          route.enabled
                            ? "truncate font-medium"
                            : "text-muted-foreground truncate font-medium"
                        }
                      >
                        {destination.name}
                      </Text>
                      {destination.sensitiveData === "include" ? (
                        <Badge variant="warning" background={false} size="sm">
                          Sensitive
                        </Badge>
                      ) : null}
                    </div>
                    <span className="mt-1 block truncate font-mono text-xs text-placeholder">
                      {destination.endpointUrl}
                    </span>
                  </div>
                  {route.enabled ? (
                    <span className="flex shrink-0 items-center gap-1.5 text-sm text-default-success">
                      <Icon name="check" className="size-3.5" />
                      Enabled
                    </span>
                  ) : (
                    <span className="shrink-0 text-sm text-placeholder">
                      Paused
                    </span>
                  )}
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function RoutesSection({
  rows,
  mutating,
  onNewRoute,
  onEditDestination,
  onToggleRoute,
  onDeleteRoute,
}: {
  rows: SourceRouteRow[];
  mutating: boolean;
  onNewRoute: () => void;
  onEditDestination: (destination: OtelDestination) => void;
  onToggleRoute: (route: DataExportRoute, enabled: boolean) => void;
  onDeleteRoute: (route: DataExportRoute) => void;
}): JSX.Element {
  const columns = useMemo<Column<SourceRouteRow>[]>(
    () => [
      {
        key: "source",
        header: "Source",
        width: "1.2fr",
        render: () => <Text className="font-medium">Product telemetry</Text>,
      },
      {
        key: "destination",
        header: "Destination",
        width: "1.2fr",
        render: ({ destination }) => (
          <Text className="truncate font-medium">{destination.name}</Text>
        ),
      },
      {
        key: "endpoint",
        header: "Endpoint",
        width: "2fr",
        render: ({ destination }) => (
          <span className="block truncate font-mono text-[13px] text-default">
            {destination.endpointUrl}
          </span>
        ),
      },
      {
        key: "sensitiveData",
        header: "Sensitive data",
        width: "1fr",
        render: ({ destination }) => (
          <SensitiveDataBadge destination={destination} />
        ),
      },
      {
        key: "status",
        header: "Status",
        width: "0.8fr",
        render: ({ route }) =>
          route.enabled ? (
            <span className="flex items-center gap-1.5 text-sm">
              <Icon name="check" className="size-3.5 text-default-success" />
              Enabled
            </span>
          ) : (
            <span className="text-sm text-placeholder">Paused</span>
          ),
      },
      {
        key: "enabled",
        header: "On",
        width: "56px",
        render: ({ route, destination }) => (
          <RequireScope scope="project:write" level="component">
            <Switch
              checked={route.enabled}
              onCheckedChange={(enabled) => onToggleRoute(route, enabled)}
              disabled={mutating}
              aria-label={`${route.enabled ? "Pause" : "Enable"} route from Product telemetry to ${destination.name}`}
            />
          </RequireScope>
        ),
      },
      {
        key: "actions",
        header: "",
        width: "44px",
        render: ({ route, destination }) => (
          <RequireScope scope="project:write" level="component">
            <MoreActions
              actions={[
                {
                  label: "Edit destination",
                  icon: "pencil",
                  onClick: () => onEditDestination(destination),
                },
                {
                  label: "Delete route",
                  icon: "trash-2",
                  destructive: true,
                  disabled: mutating,
                  onClick: () => onDeleteRoute(route),
                },
              ]}
            />
          </RequireScope>
        ),
      },
    ],
    [mutating, onDeleteRoute, onEditDestination, onToggleRoute],
  );

  return (
    <SettingsSection>
      <div className="flex items-end justify-between gap-4">
        <SettingsSection.Header>
          <SettingsSection.Title>Routes</SettingsSection.Title>
          <SettingsSection.Description>
            One source, one destination, per row. Add as many as you need.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <RequireScope scope="project:write" level="component">
          <Button variant="primary" size="sm" onClick={onNewRoute}>
            <Plus className="mr-1 size-3.5" />
            New route
          </Button>
        </RequireScope>
      </div>
      <Table
        columns={columns}
        data={rows}
        rowKey={(row) => row.route.id}
        noResultsMessage="No routes configured."
      />
    </SettingsSection>
  );
}

function DestinationsSection({
  destinations,
  routes,
  mutating,
  onNewDestination,
  onEditDestination,
  onDeleteDestination,
}: {
  destinations: OtelDestination[];
  routes: DataExportRoute[];
  mutating: boolean;
  onNewDestination: () => void;
  onEditDestination: (destination: OtelDestination) => void;
  onDeleteDestination: (destination: OtelDestination) => void;
}): JSX.Element {
  const routesByDestination = useMemo(() => {
    const grouped = new Map<string, DataExportRoute[]>();
    for (const route of routes) {
      if (!route.otelDestinationId) continue;
      const current = grouped.get(route.otelDestinationId) ?? [];
      current.push(route);
      grouped.set(route.otelDestinationId, current);
    }
    return grouped;
  }, [routes]);
  const columns = useMemo<Column<OtelDestination>[]>(
    () => [
      {
        key: "name",
        header: "Name",
        width: "1.2fr",
        render: (destination) => (
          <Text className="truncate font-medium">{destination.name}</Text>
        ),
      },
      {
        key: "endpoint",
        header: "Endpoint",
        width: "2.2fr",
        render: (destination) => (
          <span className="block truncate font-mono text-[13px] text-default">
            {destination.endpointUrl}
          </span>
        ),
      },
      {
        key: "headers",
        header: "Headers",
        width: "1.5fr",
        render: (destination) => (
          <Text className="truncate text-sm">
            {destination.headers.map((header) => header.name).join(", ") ||
              "None"}
          </Text>
        ),
      },
      {
        key: "sensitiveData",
        header: "Sensitive data",
        width: "1fr",
        render: (destination) => (
          <SensitiveDataBadge destination={destination} />
        ),
      },
      {
        key: "sources",
        header: "Routed from",
        width: "1.1fr",
        render: (destination) => {
          const destinationRoutes =
            routesByDestination.get(destination.id) ?? [];
          return destinationRoutes.length > 0 ? (
            <Text className="text-sm">Product telemetry</Text>
          ) : (
            <span className="text-sm text-placeholder">Nothing yet</span>
          );
        },
      },
      {
        key: "actions",
        header: "",
        width: "44px",
        render: (destination) => {
          const isRouted =
            (routesByDestination.get(destination.id)?.length ?? 0) > 0;
          return (
            <RequireScope scope="project:write" level="component">
              <MoreActions
                actions={[
                  {
                    label: "Edit destination",
                    icon: "pencil",
                    onClick: () => onEditDestination(destination),
                  },
                  {
                    label: "Delete destination",
                    icon: "trash-2",
                    destructive: true,
                    disabled: isRouted || mutating,
                    description: isRouted
                      ? "Remove it from every source before deleting."
                      : undefined,
                    onClick: () => onDeleteDestination(destination),
                  },
                ]}
              />
            </RequireScope>
          );
        },
      },
    ],
    [mutating, onDeleteDestination, onEditDestination, routesByDestination],
  );

  return (
    <SettingsSection>
      <div className="flex items-end justify-between gap-4">
        <SettingsSection.Header>
          <SettingsSection.Title>Destinations</SettingsSection.Title>
          <SettingsSection.Description>
            OTLP base URLs. Signal-specific paths are appended during delivery.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <RequireScope scope="project:write" level="component">
          <Button variant="primary" size="sm" onClick={onNewDestination}>
            <Plus className="mr-1 size-3.5" />
            New destination
          </Button>
        </RequireScope>
      </div>
      <Table
        columns={columns}
        data={destinations}
        rowKey={(destination) => destination.id}
      />
    </SettingsSection>
  );
}

function DeleteDestinationDialog({
  destination,
  deleting,
  onOpenChange,
  onConfirm,
}: {
  destination?: OtelDestination;
  deleting: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}): JSX.Element {
  return (
    <Dialog open={destination !== undefined} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete {destination?.name}</Dialog.Title>
          <Dialog.Description>
            This removes the destination configuration and its encrypted
            headers. This action cannot be undone.
          </Dialog.Description>
        </Dialog.Header>
        <Dialog.Footer>
          <Button
            type="button"
            variant="secondary"
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive-primary"
            disabled={deleting}
            onClick={onConfirm}
          >
            {deleting ? "Deleting" : "Delete destination"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
