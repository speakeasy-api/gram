import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
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
import { RequireScope } from "@/components/require-scope";
import { WriteOnlyHeaderRow } from "@/components/write-only-header-row";
import {
  blankWriteOnlyHeader,
  editableHeaderFromServer,
  hasValidWriteOnlyHeaders,
  preservesStoredHeaderValue,
  type EditableWriteOnlyHeader,
} from "@/lib/write-only-headers";
import { useForm } from "@tanstack/react-form";
import type { OtelDestination } from "@gram/client/models/components/oteldestination.js";
import type { DataExportRoute } from "@gram/client/models/components/dataexportroute.js";
import { Plus } from "lucide-react";
import { useMemo } from "react";

export type DestinationFormValues = {
  name: string;
  endpointUrl: string;
  includeSensitiveData: boolean;
  headers: EditableWriteOnlyHeader[];
};

function valuesForDestination(
  destination: OtelDestination | undefined,
): DestinationFormValues {
  return {
    name: destination?.name ?? "",
    endpointUrl: destination?.endpointUrl ?? "",
    includeSensitiveData: destination?.sensitiveData === "include",
    headers: destination?.headers.map(editableHeaderFromServer) ?? [],
  };
}

export function DestinationEditorSheet({
  destination,
  routes,
  saving,
  deleting,
  onClose,
  onSave,
  onRequestDelete,
}: {
  destination?: OtelDestination;
  routes: DataExportRoute[];
  saving: boolean;
  deleting: boolean;
  onClose: () => void;
  onSave: (values: DestinationFormValues) => Promise<void>;
  onRequestDelete: (destination: OtelDestination) => void;
}): JSX.Element {
  const defaultValues = useMemo(
    () => valuesForDestination(destination),
    [destination],
  );
  const form = useForm({
    defaultValues,
    onSubmit: async ({ value }) => {
      await onSave(value);
    },
  });
  const isEditing = destination !== undefined;
  const isRouted = routes.length > 0;
  const formDisabled = saving || deleting;

  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open && !formDisabled && !form.state.isSubmitting) onClose();
      }}
    >
      <SheetContent
        side="right"
        className="w-[520px] max-w-[calc(100vw-2rem)] gap-0 bg-card p-0 sm:max-w-[520px]"
      >
        <SheetHeader className="border-b px-6 py-5">
          <SheetTitle>{destination?.name ?? "New destination"}</SheetTitle>
          <SheetDescription>
            A reusable connection to a collector you own.
          </SheetDescription>
        </SheetHeader>

        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            void form.handleSubmit();
          }}
        >
          <form.Subscribe selector={(state) => state.isSubmitting}>
            {(isSubmitting) => {
              const disabled = formDisabled || isSubmitting;
              return (
                <fieldset disabled={disabled} className="contents">
                  <div className="min-h-0 flex-1 overflow-y-auto px-6">
                    <div className="space-y-2 py-5">
                      <form.Field name="name">
                        {(field) => (
                          <>
                            <Label htmlFor={field.name}>Name</Label>
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
                      <Text muted className="text-xs leading-relaxed">
                        What this collector is called when you pick it on a
                        source.
                      </Text>
                    </div>

                    <div className="space-y-2 border-t py-5">
                      <form.Field name="endpointUrl">
                        {(field) => (
                          <>
                            <Label htmlFor={field.name}>Endpoint URL</Label>
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
                        HTTPS OTLP base URL. Signal-specific paths are appended
                        during delivery.
                      </Text>
                    </div>

                    <form.Field name="headers" mode="array">
                      {(headersField) => (
                        <div className="space-y-3 border-t py-5">
                          <div className="flex items-center justify-between gap-3">
                            <Label>Headers</Label>
                            <RequireScope
                              scope="project:write"
                              level="component"
                            >
                              <Button
                                type="button"
                                variant="secondary"
                                size="sm"
                                onClick={() =>
                                  headersField.pushValue(blankWriteOnlyHeader())
                                }
                              >
                                <Plus className="mr-1 size-3.5" />
                                Add header
                              </Button>
                            </RequireScope>
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
                                          hasStoredValue={
                                            header.hasStoredValue &&
                                            preservesStoredHeaderValue(header)
                                          }
                                          nameInputName={nameField.name}
                                          valueInputName={valueField.name}
                                          disabled={disabled}
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
                          <Text muted className="text-xs leading-relaxed">
                            Values are encrypted at rest and never returned by
                            the API. Leave a stored value blank to keep it.
                          </Text>
                        </div>
                      )}
                    </form.Field>

                    <div className="space-y-3 border-t py-5">
                      <form.Field name="includeSensitiveData">
                        {(field) => (
                          <>
                            <div className="flex items-start justify-between gap-4">
                              <div className="space-y-1">
                                <Label>Include sensitive data</Label>
                                <Text muted className="text-sm">
                                  Send tool arguments, results and prompt
                                  content to this destination.
                                </Text>
                              </div>
                              <RequireScope
                                scope="project:write"
                                level="component"
                              >
                                <Switch
                                  checked={field.state.value}
                                  onCheckedChange={field.handleChange}
                                  aria-label="Include sensitive data"
                                />
                              </RequireScope>
                            </div>
                            {field.state.value ? (
                              <Alert variant="warning" alignTop>
                                <Text className="text-sm">
                                  Payloads sent here will contain customer data.
                                  This applies to every source routed to this
                                  destination.
                                </Text>
                              </Alert>
                            ) : null}
                          </>
                        )}
                      </form.Field>
                    </div>

                    {isEditing ? (
                      <div className="space-y-3 border-t py-5">
                        <div>
                          <Label>Routed from</Label>
                          <Text muted className="mt-1 text-xs leading-relaxed">
                            Routes are managed on the source. A destination in
                            use cannot be deleted.
                          </Text>
                        </div>
                        {routes.length === 0 ? (
                          <Text muted className="border p-3 text-sm">
                            Nothing yet
                          </Text>
                        ) : (
                          routes.map((route) => (
                            <div
                              key={route.id}
                              className="flex items-center justify-between border px-3 py-2"
                            >
                              <Text className="text-sm font-medium">
                                Product telemetry
                              </Text>
                              <span className="text-eyebrow text-muted-foreground">
                                {route.enabled ? "On" : "Paused"}
                              </span>
                            </div>
                          ))
                        )}
                      </div>
                    ) : null}
                  </div>

                  <form.Subscribe
                    selector={(state) =>
                      [
                        state.values.name,
                        state.values.endpointUrl,
                        state.values.headers,
                      ] as const
                    }
                  >
                    {([name, endpointUrl, headers]) => {
                      let endpointIsValid = false;
                      try {
                        const parsedEndpoint = new URL(endpointUrl.trim());
                        endpointIsValid =
                          parsedEndpoint.protocol === "https:" &&
                          parsedEndpoint.hostname !== "";
                      } catch {
                        endpointIsValid = false;
                      }

                      const canSave =
                        name.trim() !== "" &&
                        endpointIsValid &&
                        hasValidWriteOnlyHeaders(headers) &&
                        !disabled;

                      return (
                        <SheetFooter className="min-h-14 flex-row items-center justify-between border-t bg-muted/30 px-6 py-3">
                          <div>
                            {destination ? (
                              <RequireScope
                                scope="project:write"
                                level="component"
                              >
                                <Button
                                  type="button"
                                  variant="destructive-secondary"
                                  size="sm"
                                  disabled={isRouted || disabled}
                                  tooltip={
                                    isRouted
                                      ? "Remove this destination from every source before deleting it."
                                      : undefined
                                  }
                                  onClick={() => onRequestDelete(destination)}
                                >
                                  Delete
                                </Button>
                              </RequireScope>
                            ) : null}
                          </div>
                          <div className="flex items-center gap-2">
                            <Button
                              type="button"
                              variant="secondary"
                              size="sm"
                              onClick={onClose}
                            >
                              Cancel
                            </Button>
                            <RequireScope
                              scope="project:write"
                              level="component"
                            >
                              <Button
                                type="submit"
                                variant="primary"
                                size="sm"
                                disabled={!canSave}
                              >
                                {saving
                                  ? "Saving"
                                  : destination
                                    ? "Save"
                                    : "Create destination"}
                              </Button>
                            </RequireScope>
                          </div>
                        </SheetFooter>
                      );
                    }}
                  </form.Subscribe>
                </fieldset>
              );
            }}
          </form.Subscribe>
        </form>
      </SheetContent>
    </Sheet>
  );
}
