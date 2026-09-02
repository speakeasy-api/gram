import { ProjectAvatar } from "@/components/project-menu";
import { RequireScope } from "@/components/require-scope";
import { WriteOnlyHeaderRow } from "@/components/write-only-header-row";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Stack } from "@/components/ui/Stack";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import {
  blankWriteOnlyHeader,
  hasValidWriteOnlyHeaders,
  type EditableWriteOnlyHeader,
} from "@/lib/write-only-headers";
import { useForm } from "@tanstack/react-form";
import {
  DataSource,
  type DataSource as DataSourceValue,
} from "@gram/client/models/components/createdataexportrouteform.js";
import type { DataExportRoute } from "@gram/client/models/components/dataexportroute.js";
import type { Destination } from "@gram/client/models/components/destination.js";
import type { OtelDestination } from "@gram/client/models/components/oteldestination.js";
import type { ProjectEntry } from "@gram/client/models/components/projectentry.js";
import { Plus } from "lucide-react";
import { useId, useMemo } from "react";

const NEW_DESTINATION = "__new_destination__";

export type OtelDataExportDestination = Destination & {
  destinationType: "otel";
  otel: OtelDestination;
};

type ProjectOption = DropdownItem & { project: ProjectEntry };
type DestinationOption = DropdownItem & {
  destination?: OtelDataExportDestination;
  createNew?: boolean;
};

export type ConfigureExportValues = {
  projectSlug: string;
  dataSource: DataSourceValue;
  destinationId: string;
  enabled: boolean;
  destinationName: string;
  endpointUrl: string;
  includeSensitiveData: boolean;
  headers: EditableWriteOnlyHeader[];
};

function isValidEndpointURL(value: string): boolean {
  try {
    const protocol = new URL(value).protocol;
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

function initialDestinationID(
  route: DataExportRoute | undefined,
  destinations: OtelDataExportDestination[],
): string {
  if (
    route?.otelDestinationId &&
    destinations.some(
      (destination) => destination.id === route.otelDestinationId,
    )
  ) {
    return route.otelDestinationId;
  }
  return destinations[0]?.id ?? NEW_DESTINATION;
}

function destinationSelectionIsValid(
  values: ConfigureExportValues,
  destinations: OtelDataExportDestination[],
): boolean {
  if (values.destinationId !== NEW_DESTINATION) {
    return destinations.some(
      (destination) => destination.id === values.destinationId,
    );
  }
  return (
    values.destinationName.trim() !== "" &&
    isValidEndpointURL(values.endpointUrl.trim()) &&
    hasValidWriteOnlyHeaders(values.headers)
  );
}

function submitLabel(
  loading: boolean,
  saving: boolean,
  route: DataExportRoute | undefined,
): string {
  if (loading) return "Loading project";
  if (saving) return "Saving";
  return route ? "Save export" : "Create export";
}

export function ConfigureExportSheet({
  projects,
  project,
  destinations,
  route,
  saving,
  loading,
  onClose,
  onProjectChange,
  onSave,
}: {
  projects: ProjectEntry[];
  project: ProjectEntry;
  destinations: OtelDataExportDestination[];
  route?: DataExportRoute;
  saving: boolean;
  loading: boolean;
  onClose: () => void;
  onProjectChange: (projectSlug: string) => void;
  onSave: (values: ConfigureExportValues) => Promise<void>;
}): JSX.Element {
  const projectControlId = useId();
  const sourceControlId = useId();
  const destinationControlId = useId();
  const projectOptions = useMemo<ProjectOption[]>(
    () =>
      projects.map((candidate) => ({
        value: candidate.slug,
        label: candidate.name,
        project: candidate,
        icon: (
          <ProjectAvatar
            project={candidate}
            className="size-4 min-h-4 min-w-4"
          />
        ),
      })),
    [projects],
  );
  const selectedProject = projectOptions.find(
    (option) => option.value === project.slug,
  );
  const destinationOptions = useMemo<DestinationOption[]>(
    () => [
      ...destinations.map((destination) => ({
        value: destination.id,
        label: destination.name,
        description: destination.otel.endpointUrl,
        keywords: [destination.otel.endpointUrl],
        destination,
      })),
      {
        value: NEW_DESTINATION,
        label: "Create a new destination",
        icon: <Plus className="size-4" />,
        createNew: true,
      },
    ],
    [destinations],
  );
  const initialDestinationId = initialDestinationID(route, destinations);
  const form = useForm({
    defaultValues: {
      projectSlug: project.slug,
      dataSource: DataSource.ProductTelemetry,
      destinationId: initialDestinationId,
      enabled: (route?.enabled ?? true) as boolean,
      destinationName: "",
      endpointUrl: "",
      includeSensitiveData: false as boolean,
      headers: [] as EditableWriteOnlyHeader[],
    } satisfies ConfigureExportValues,
    onSubmit: async ({ value }) => onSave(value),
  });

  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open && !saving) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="w-[560px] max-w-[calc(100vw-2rem)] gap-0 bg-card p-0 sm:max-w-[560px]"
      >
        <SheetHeader className="border-b px-6 py-5">
          <SheetTitle>{route ? "Configure export" : "New export"}</SheetTitle>
          <SheetDescription>
            Choose the project, what to send, and where it should go.
          </SheetDescription>
        </SheetHeader>

        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          <div className="min-h-0 flex-1 overflow-y-auto px-6">
            <div className="space-y-3 py-5">
              <Label htmlFor={projectControlId}>Project</Label>
              <Combobox
                id={projectControlId}
                items={projectOptions}
                selected={selectedProject}
                onSelectionChange={(option) => onProjectChange(option.value)}
                variant="secondary"
                className="h-10 w-full"
                contentClassName="w-[min(480px,calc(100vw-2rem))]"
                searchable
                searchPlaceholder="Search projects"
              >
                <div className="flex min-w-0 items-center gap-2">
                  <ProjectAvatar
                    project={project}
                    className="size-4 min-h-4 min-w-4"
                  />
                  <span className="truncate">{project.name}</span>
                </div>
              </Combobox>
            </div>

            <form.Field name="dataSource">
              {(field) => (
                <div className="space-y-3 border-t py-5">
                  <Label htmlFor={sourceControlId}>Data to export</Label>
                  <Select
                    value={field.state.value}
                    onValueChange={(value) =>
                      field.handleChange(value as DataSourceValue)
                    }
                  >
                    <SelectTrigger id={sourceControlId} className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value={DataSource.ProductTelemetry}>
                        Product telemetry
                      </SelectItem>
                    </SelectContent>
                  </Select>
                  <Text muted className="text-sm leading-relaxed">
                    OTLP traces and logs from MCP servers and tool calls.
                  </Text>
                </div>
              )}
            </form.Field>

            <form.Field name="destinationId">
              {(field) => {
                const selected = destinationOptions.find(
                  (option) => option.value === field.state.value,
                );
                return (
                  <div className="space-y-3 border-t py-5">
                    <Label htmlFor={destinationControlId}>Send to</Label>
                    <Combobox
                      id={destinationControlId}
                      items={destinationOptions}
                      selected={selected}
                      onSelectionChange={(option) =>
                        field.handleChange(option.value)
                      }
                      variant="secondary"
                      className="h-10 w-full"
                      contentClassName="w-[min(480px,calc(100vw-2rem))]"
                      searchable
                      searchPlaceholder="Search destinations"
                    >
                      {selected?.destination?.name ??
                        selected?.label ??
                        "Select a destination"}
                    </Combobox>
                    {selected?.destination ? (
                      <div className="border bg-muted/20 px-4 py-3">
                        <Text className="font-medium">
                          {selected.destination.name}
                        </Text>
                        <span className="mt-1 block truncate font-mono text-xs text-placeholder">
                          {selected.destination.otel.endpointUrl}
                        </span>
                      </div>
                    ) : null}
                  </div>
                );
              }}
            </form.Field>

            <form.Subscribe
              selector={(state) =>
                state.values.destinationId === NEW_DESTINATION
              }
            >
              {(creatingDestination) => {
                if (!creatingDestination) return null;
                return (
                  <div className="space-y-5 border-t py-5">
                    <div className="space-y-2">
                      <form.Field name="destinationName">
                        {(field) => (
                          <>
                            <Label htmlFor={field.name}>Destination name</Label>
                            <Input
                              id={field.name}
                              name={field.name}
                              value={field.state.value}
                              onChange={field.handleChange}
                              onBlur={field.handleBlur}
                              placeholder="Datadog"
                            />
                          </>
                        )}
                      </form.Field>
                    </div>

                    <div className="space-y-2">
                      <form.Field name="endpointUrl">
                        {(field) => (
                          <>
                            <Label htmlFor={field.name}>OTLP endpoint</Label>
                            <Input
                              id={field.name}
                              name={field.name}
                              value={field.state.value}
                              onChange={field.handleChange}
                              onBlur={field.handleBlur}
                              placeholder="https://collector.example.com"
                              className="font-mono"
                            />
                          </>
                        )}
                      </form.Field>
                      <Text muted className="text-xs leading-relaxed">
                        HTTP or HTTPS. Signal-specific paths are appended during
                        delivery.
                      </Text>
                    </div>

                    <form.Field name="headers" mode="array">
                      {(headersField) => (
                        <div className="space-y-3">
                          <div className="flex items-center justify-between gap-3">
                            <Label>Headers</Label>
                            <Button
                              type="button"
                              variant="secondary"
                              size="sm"
                              onClick={() =>
                                headersField.pushValue(blankWriteOnlyHeader())
                              }
                            >
                              <Button.LeftIcon>
                                <Plus className="size-3.5" />
                              </Button.LeftIcon>
                              Add header
                            </Button>
                          </div>
                          {headersField.state.value.length === 0 ? (
                            <Text muted className="text-sm">
                              No custom headers.
                            </Text>
                          ) : (
                            <Stack gap={2}>
                              {headersField.state.value.map((header, index) => (
                                <form.Field
                                  key={header.rowID}
                                  name={`headers[${index}].name` as const}
                                >
                                  {(nameField) => (
                                    <form.Field
                                      name={`headers[${index}].value` as const}
                                    >
                                      {(valueField) => (
                                        <WriteOnlyHeaderRow
                                          name={nameField.state.value}
                                          value={valueField.state.value}
                                          hasStoredValue={false}
                                          nameInputName={nameField.name}
                                          valueInputName={valueField.name}
                                          nameInputLabel={`Header ${index + 1} name`}
                                          valueInputLabel={`Header ${index + 1} value`}
                                          disabled={saving}
                                          onNameChange={nameField.handleChange}
                                          onNameBlur={nameField.handleBlur}
                                          onValueChange={
                                            valueField.handleChange
                                          }
                                          onValueBlur={valueField.handleBlur}
                                          onRemove={() =>
                                            headersField.removeValue(index)
                                          }
                                        />
                                      )}
                                    </form.Field>
                                  )}
                                </form.Field>
                              ))}
                            </Stack>
                          )}
                        </div>
                      )}
                    </form.Field>

                    <form.Field name="includeSensitiveData">
                      {(field) => (
                        <div className="space-y-3">
                          <div className="flex items-start justify-between gap-4">
                            <div className="space-y-1">
                              <Label>Include sensitive data</Label>
                              <Text muted className="text-sm">
                                Send tool arguments, results, and prompt
                                content.
                              </Text>
                            </div>
                            <Switch
                              checked={field.state.value}
                              onCheckedChange={field.handleChange}
                              aria-label="Include sensitive data"
                            />
                          </div>
                          {field.state.value ? (
                            <Alert variant="warning" alignTop>
                              This export may contain customer data.
                            </Alert>
                          ) : null}
                        </div>
                      )}
                    </form.Field>
                  </div>
                );
              }}
            </form.Subscribe>

            <form.Field name="enabled">
              {(field) => (
                <div className="flex items-start justify-between gap-4 border-t py-5">
                  <div className="space-y-1">
                    <Label>Start exporting</Label>
                    <Text muted className="text-sm">
                      Turn off to save this export paused.
                    </Text>
                  </div>
                  <Switch
                    checked={field.state.value}
                    onCheckedChange={field.handleChange}
                    aria-label="Start exporting"
                  />
                </div>
              )}
            </form.Field>
          </div>

          <form.Subscribe
            selector={(state) => [state.values, state.isSubmitting] as const}
          >
            {([values, isSubmitting]) => {
              const destinationValid = destinationSelectionIsValid(
                values,
                destinations,
              );
              const disabled =
                loading || saving || isSubmitting || !destinationValid;
              return (
                <SheetFooter className="min-h-14 flex-row items-center justify-end gap-2 border-t bg-muted/30 px-6 py-3">
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    onClick={onClose}
                    disabled={saving || isSubmitting}
                  >
                    Cancel
                  </Button>
                  <RequireScope scope="org:admin" level="component">
                    <Button
                      type="submit"
                      variant="primary"
                      size="sm"
                      disabled={disabled}
                    >
                      {submitLabel(loading, saving, route)}
                    </Button>
                  </RequireScope>
                </SheetFooter>
              );
            }}
          </form.Subscribe>
        </form>
      </SheetContent>
    </Sheet>
  );
}
