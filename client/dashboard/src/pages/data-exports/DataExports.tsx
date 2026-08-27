import { InlineEmptyState } from "@/components/inline-empty-state";
import { SettingsPage, SettingsSection } from "@/components/page-templates";
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
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { writeOnlyHeaderInput } from "@/lib/write-only-headers";
import { useQueryClient } from "@tanstack/react-query";
import type { DataExportRoute } from "@gram/client/models/components/dataexportroute.js";
import type { ListDataExportRoutesResult } from "@gram/client/models/components/listdataexportroutesresult.js";
import type { OtelDestination } from "@gram/client/models/components/oteldestination.js";
import { useCreateDataExportRouteMutation } from "@gram/client/react-query/createDataExportRoute.js";
import { useCreateOtelDestinationMutation } from "@gram/client/react-query/createOtelDestination.js";
import {
  invalidateAllDataExportRoutes,
  useDataExportRoutes,
} from "@gram/client/react-query/dataExportRoutes.js";
import { useDeleteDataExportRouteMutation } from "@gram/client/react-query/deleteDataExportRoute.js";
import { useDeleteOtelDestinationMutation } from "@gram/client/react-query/deleteOtelDestination.js";
import {
  invalidateAllOtelDestinations,
  useOtelDestinations,
} from "@gram/client/react-query/otelDestinations.js";
import { useUpdateDataExportRouteMutation } from "@gram/client/react-query/updateDataExportRoute.js";
import { useUpdateOtelDestinationMutation } from "@gram/client/react-query/updateOtelDestination.js";
import { Plus } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  DestinationEditorSheet,
  type DestinationFormValues,
} from "./DestinationEditorSheet";

const OTEL_FORWARDING_SOURCE = "otel_forwarding" as const;
const NEW_DESTINATION_VALUE = "__new_destination__";
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

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Unexpected error";
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
  const destinationsQuery = useOtelDestinations();
  const routesQuery = useDataExportRoutes();
  const createDestination = useCreateOtelDestinationMutation();
  const updateDestination = useUpdateOtelDestinationMutation();
  const deleteDestination = useDeleteOtelDestinationMutation();
  const createRoute = useCreateDataExportRouteMutation();
  const updateRoute = useUpdateDataExportRouteMutation();
  const deleteRoute = useDeleteDataExportRouteMutation();

  const [editor, setEditor] = useState<{
    destination?: OtelDestination;
    routeAfterCreate: boolean;
  }>();
  const [deleteCandidate, setDeleteCandidate] = useState<OtelDestination>();

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

  const invalidateLists = useCallback(
    async () =>
      Promise.all([
        invalidateAllOtelDestinations(queryClient),
        invalidateAllDataExportRoutes(queryClient),
      ]),
    [queryClient],
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

      if (editor?.routeAfterCreate) {
        try {
          await createRoute.mutateAsync({
            request: {
              createDataExportRouteForm: {
                dataSource: OTEL_FORWARDING_SOURCE,
                enabled: true,
                otelDestinationId: saved.id,
              },
            },
          });
        } catch (error) {
          toast.error(
            `Destination created, but routing failed: ${errorMessage(error)}`,
          );
        }
      }

      await invalidateLists();
      setEditor(undefined);
    } catch (error) {
      toast.error(`Failed to save destination: ${errorMessage(error)}`);
    }
  };

  const handleDeleteDestination = async () => {
    if (!deleteCandidate) return;
    try {
      await deleteDestination.mutateAsync({
        request: { id: deleteCandidate.id },
      });
      await invalidateLists();
      toast.success("Destination deleted");
      setDeleteCandidate(undefined);
      setEditor(undefined);
    } catch (error) {
      toast.error(`Failed to delete destination: ${errorMessage(error)}`);
    }
  };

  const handleCreateRoute = async (destination: OtelDestination) => {
    try {
      await createRoute.mutateAsync({
        request: {
          createDataExportRouteForm: {
            dataSource: OTEL_FORWARDING_SOURCE,
            enabled: true,
            otelDestinationId: destination.id,
          },
        },
      });
      await invalidateLists();
      toast.success(`${destination.name} added to Product telemetry`);
    } catch (error) {
      toast.error(`Failed to add destination: ${errorMessage(error)}`);
    }
  };

  const handleToggleRoute = async (
    route: DataExportRoute,
    enabled: boolean,
  ) => {
    const previous = queryClient.getQueriesData<ListDataExportRoutesResult>({
      queryKey: ["@gram/client", "dataExports", "listRoutes"],
    });
    queryClient.setQueriesData<ListDataExportRoutesResult>(
      { queryKey: ["@gram/client", "dataExports", "listRoutes"] },
      (current) =>
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
      for (const [key, value] of previous) {
        queryClient.setQueryData(key, value);
      }
      toast.error(`Failed to update route: ${errorMessage(error)}`);
    } finally {
      await invalidateLists();
    }
  };

  const handleDeleteRoute = async (route: DataExportRoute) => {
    try {
      await deleteRoute.mutateAsync({ request: { id: route.id } });
      await invalidateLists();
      toast.success("Destination removed from Product telemetry");
    } catch (error) {
      toast.error(`Failed to remove destination: ${errorMessage(error)}`);
    }
  };

  const loading = destinationsQuery.isPending || routesQuery.isPending;
  const error = destinationsQuery.error ?? routesQuery.error;
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
      {error ? (
        <Alert variant="error">
          Unable to load data exports: {errorMessage(error)}
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
                      routeAfterCreate: false,
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
          <SourcesSection
            destinations={destinations}
            rows={routedRows}
            mutating={mutating}
            onAddDestination={(destination) =>
              void handleCreateRoute(destination)
            }
            onNewDestination={() =>
              setEditor({ destination: undefined, routeAfterCreate: true })
            }
            onEditDestination={(destination) =>
              setEditor({ destination, routeAfterCreate: false })
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
              setEditor({ destination: undefined, routeAfterCreate: false })
            }
            onEditDestination={(destination) =>
              setEditor({ destination, routeAfterCreate: false })
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

type DestinationPickerItem = DropdownItem & {
  destination?: OtelDestination;
  createNew?: boolean;
};

function SourcesSection({
  destinations,
  rows,
  mutating,
  onAddDestination,
  onNewDestination,
  onEditDestination,
  onToggleRoute,
  onDeleteRoute,
}: {
  destinations: OtelDestination[];
  rows: SourceRouteRow[];
  mutating: boolean;
  onAddDestination: (destination: OtelDestination) => void;
  onNewDestination: () => void;
  onEditDestination: (destination: OtelDestination) => void;
  onToggleRoute: (route: DataExportRoute, enabled: boolean) => void;
  onDeleteRoute: (route: DataExportRoute) => void;
}): JSX.Element {
  const routedIDs = useMemo(
    () => new Set(rows.map((row) => row.destination.id)),
    [rows],
  );
  const pickerItems = useMemo<DestinationPickerItem[]>(
    () => [
      ...destinations
        .filter((destination) => !routedIDs.has(destination.id))
        .map((destination) => ({
          value: destination.id,
          label: destination.name,
          keywords: [destination.endpointUrl],
          destination,
        })),
      {
        value: NEW_DESTINATION_VALUE,
        label: "New destination",
        createNew: true,
      },
    ],
    [destinations, routedIDs],
  );
  const columns = useMemo<Column<SourceRouteRow>[]>(
    () => [
      {
        key: "destination",
        header: "Destination",
        width: "1.5fr",
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
        width: "1fr",
        render: ({ route }) => (
          <span className="flex items-center gap-1.5 text-sm">
            {route.enabled ? (
              <>
                <Icon name="check" className="size-3.5 text-default-success" />
                Enabled
              </>
            ) : (
              <span className="text-placeholder">Paused</span>
            )}
          </span>
        ),
      },
      {
        key: "enabled",
        header: "On",
        width: "56px",
        render: ({ route }) => (
          <RequireScope scope="project:write" level="component">
            <Switch
              checked={route.enabled}
              onCheckedChange={(enabled) => onToggleRoute(route, enabled)}
              disabled={mutating}
              aria-label={`Toggle ${route.id}`}
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
                  label: "Remove from source",
                  icon: "trash-2",
                  destructive: true,
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
      <SettingsSection.Header>
        <SettingsSection.Title>Sources</SettingsSection.Title>
        <SettingsSection.Description>
          Each source can fan out to any number of destinations.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <div className="flex items-center justify-between gap-4 border-b px-6 py-[18px]">
          <div>
            <Text className="font-medium">Product telemetry</Text>
            <Text muted className="mt-1 text-[13px]">
              OTLP traces and logs from every MCP server and tool call in this
              project.
            </Text>
          </div>
          <RequireScope scope="project:write" level="component">
            <Combobox
              items={pickerItems}
              selected={undefined}
              onSelectionChange={(item) => {
                if (item.createNew) onNewDestination();
                else if (item.destination) onAddDestination(item.destination);
              }}
              disabledMessage={
                mutating ? "A route update is in progress." : undefined
              }
              contentClassName="w-[280px]"
              searchable
              searchPlaceholder="Find a destination"
            >
              <span className="flex items-center gap-1.5">
                <Plus className="size-3.5" />
                Add destination
              </span>
            </Combobox>
          </RequireScope>
        </div>
        <Table
          columns={columns}
          data={rows}
          rowKey={(row) => row.route.id}
          noResultsMessage="No destinations routed from Product telemetry."
        />
        <div className="flex items-center gap-2 border-t bg-surface-secondary-default px-6 py-3 text-placeholder">
          <Icon name="info" className="size-3.5" />
          <Text className="text-xs">
            Risk findings, agent sessions and tool calls will appear here as
            sources when they ship.
          </Text>
        </div>
      </SettingsSection.Panel>
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
