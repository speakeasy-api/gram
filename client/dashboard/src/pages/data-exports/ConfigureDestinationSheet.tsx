import { RequireScope } from "@/components/require-scope";
import { WriteOnlyHeaderRow } from "@/components/write-only-header-row";
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
import {
  blankWriteOnlyHeader,
  editableHeaderFromServer,
  hasValidWriteOnlyHeaders,
  type EditableWriteOnlyHeader,
} from "@/lib/write-only-headers";
import { useForm } from "@tanstack/react-form";
import { Plus } from "lucide-react";
import type { OtelDataExportDestination } from "./ConfigureExportSheet";

function isValidDataExportEndpointURL(value: string): boolean {
  try {
    const protocol = new URL(value).protocol;
    return protocol === "http:" || protocol === "https:";
  } catch {
    return false;
  }
}

export type ConfigureDestinationValues = {
  destinationName: string;
  endpointUrl: string;
  includeSensitiveData: boolean;
  headers: EditableWriteOnlyHeader[];
};

function destinationValuesAreValid(
  values: ConfigureDestinationValues,
): boolean {
  return (
    values.destinationName.trim() !== "" &&
    isValidDataExportEndpointURL(values.endpointUrl.trim()) &&
    hasValidWriteOnlyHeaders(values.headers)
  );
}

export function ConfigureDestinationSheet({
  destination,
  saving,
  onClose,
  onSave,
}: {
  destination: OtelDataExportDestination;
  saving: boolean;
  onClose: () => void;
  onSave: (values: ConfigureDestinationValues) => Promise<void>;
}): JSX.Element {
  const form = useForm({
    defaultValues: {
      destinationName: destination.name,
      endpointUrl: destination.otel.endpointUrl,
      includeSensitiveData: destination.sensitiveData === "include",
      headers: destination.otel.headers.map(editableHeaderFromServer),
    } satisfies ConfigureDestinationValues,
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
          <SheetTitle>Configure destination</SheetTitle>
          <SheetDescription>
            Changes apply to every export using this destination.
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
            <div className="space-y-2 py-5">
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

            <div className="space-y-2 border-t py-5">
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
                <div className="space-y-3 border-t py-5">
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
                                  hasStoredValue={header.hasStoredValue}
                                  nameInputName={nameField.name}
                                  valueInputName={valueField.name}
                                  nameInputLabel={`Header ${index + 1} name`}
                                  valueInputLabel={`Header ${index + 1} value`}
                                  disabled={saving}
                                  onNameChange={nameField.handleChange}
                                  onNameBlur={nameField.handleBlur}
                                  onValueChange={valueField.handleChange}
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
                <div className="space-y-3 border-t py-5">
                  <div className="flex items-start justify-between gap-4">
                    <div className="space-y-1">
                      <Label>Include sensitive data</Label>
                      <Text muted className="text-sm">
                        Send tool arguments, results, and prompt content.
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

          <form.Subscribe
            selector={(state) => [state.values, state.isSubmitting] as const}
          >
            {([values, isSubmitting]) => (
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
                    disabled={
                      saving ||
                      isSubmitting ||
                      !destinationValuesAreValid(values)
                    }
                  >
                    {saving ? "Saving" : "Save destination"}
                  </Button>
                </RequireScope>
              </SheetFooter>
            )}
          </form.Subscribe>
        </form>
      </SheetContent>
    </Sheet>
  );
}
