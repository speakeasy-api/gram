import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import {
  invalidateAllOtelForwardingConfig,
  useOtelForwardingConfig,
} from "@gram/client/react-query/otelForwardingConfig";
import { useUpsertOtelForwardingConfigMutation } from "@gram/client/react-query/upsertOtelForwardingConfig";
import { useDeleteOtelForwardingConfigMutation } from "@gram/client/react-query/deleteOtelForwardingConfig";
import type { OtelForwardingConfig } from "@gram/client/models/components/otelforwardingconfig.js";
import type { OtelForwardingHeaderInput } from "@gram/client/models/components/otelforwardingheaderinput.js";
import type { OtelForwardingHeader } from "@gram/client/models/components/otelforwardingheader.js";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { useForm } from "@tanstack/react-form";
import { Plus, Send, Trash2 } from "lucide-react";
import { useMemo } from "react";
import { toast } from "sonner";

type EditableHeader = {
  // Local-only id; stable while a row is mounted so we can edit a name in
  // place without unmounting the row.
  rowID: string;
  name: string;
  // Original server name for a write-only value this row may preserve.
  // Renaming the row requires a replacement value.
  storedName: string | null;
  value: string;
};

function rowFromServer(h: OtelForwardingHeader, idx: number): EditableHeader {
  return {
    rowID: `existing-${idx}-${h.name}`,
    name: h.name,
    value: "",
    storedName: h.hasValue ? h.name : null,
  };
}

let newRowCounter = 0;
function blankRow(): EditableHeader {
  newRowCounter += 1;
  return {
    rowID: `new-${newRowCounter}`,
    name: "",
    value: "",
    storedName: null,
  };
}

type OtelForwardingFormValues = {
  enabled: boolean;
  endpointUrl: string;
  headers: EditableHeader[];
};

function formValuesFromConfig(
  config: OtelForwardingConfig | undefined,
): OtelForwardingFormValues {
  return {
    enabled: config?.enabled ?? false,
    endpointUrl: config?.endpointUrl ?? "",
    headers: config?.headers.map(rowFromServer) ?? [],
  };
}

function preservesStoredValue(header: EditableHeader): boolean {
  return (
    header.storedName !== null &&
    header.value === "" &&
    header.name.trim().toLowerCase() === header.storedName.toLowerCase()
  );
}

function hasValidHeaders(headers: EditableHeader[]): boolean {
  return headers.every((header) => {
    if (header.name.trim() === "") return false;
    return header.value !== "" || preservesStoredValue(header);
  });
}

function headerInputFromForm(
  header: EditableHeader,
): OtelForwardingHeaderInput {
  const name = header.name.trim();
  if (preservesStoredValue(header)) return { name };
  return { name, value: header.value };
}

export function OtelForwardingSection(): JSX.Element {
  const { data, isLoading } = useOtelForwardingConfig();
  const queryClient = useQueryClient();
  const defaultValues = useMemo(() => formValuesFromConfig(data), [data]);
  const upsertMutation = useUpsertOtelForwardingConfigMutation({
    onSuccess: async () => {
      toast.success("Forwarding config saved");
      await invalidateAllOtelForwardingConfig(queryClient);
    },
    onError: (err) => {
      toast.error(`Failed to save forwarding config: ${err.message}`);
    },
  });
  const deleteMutation = useDeleteOtelForwardingConfigMutation({
    onSuccess: async () => {
      toast.success("Forwarding config deleted");
      await invalidateAllOtelForwardingConfig(queryClient);
    },
    onError: (err) => {
      toast.error(`Failed to delete forwarding config: ${err.message}`);
    },
  });
  const form = useForm({
    defaultValues,
    onSubmit: async ({ value }) => {
      try {
        const saved = await upsertMutation.mutateAsync({
          request: {
            upsertConfigRequestBody3: {
              endpointUrl: value.endpointUrl.trim(),
              enabled: value.enabled,
              headers: value.headers.map(headerInputFromForm),
            },
          },
        });
        form.reset(formValuesFromConfig(saved));
      } catch {
        return;
      }
    },
  });

  const isConfigured = Boolean(data?.id);
  const isMutating = upsertMutation.isPending || deleteMutation.isPending;

  const handleDelete = async () => {
    if (!isConfigured) return;

    try {
      await deleteMutation.mutateAsync({ request: {} });
      form.reset(formValuesFromConfig(undefined));
    } catch {
      return;
    }
  };

  return (
    <Stack gap={4}>
      <div>
        <Heading variant="h4" className="mb-2">
          OTEL forwarding
        </Heading>
        <Text muted small>
          Forward a copy of every OTEL payload received on the hooks endpoint to
          your own collector. Headers are encrypted at rest; values are never
          returned by the API.
        </Text>
      </div>

      <form
        className="border-border bg-card flex flex-col gap-4 border p-4"
        onSubmit={(event) => {
          event.preventDefault();
          void form.handleSubmit();
        }}
      >
        <form.Subscribe
          selector={(state) => [state.values, state.isSubmitting] as const}
        >
          {([values, isSubmitting]) => {
            const formDisabled = isLoading || isMutating || isSubmitting;
            const canSave =
              values.endpointUrl.trim() !== "" &&
              hasValidHeaders(values.headers) &&
              !formDisabled;

            return (
              <fieldset disabled={formDisabled} className="contents">
                <Stack
                  direction="horizontal"
                  justify="space-between"
                  align="center"
                >
                  <Stack gap={1}>
                    <Stack direction="horizontal" align="center" gap={2}>
                      <Send className="text-muted-foreground h-4 w-4" />
                      <Text variant="body" className="font-medium">
                        Enable forwarding
                      </Text>
                    </Stack>
                    <Text
                      variant="body"
                      className="text-muted-foreground ml-6 text-sm"
                    >
                      Send each inbound OTEL payload to the endpoint below.
                    </Text>
                  </Stack>
                  <RequireScope scope="org:admin" level="component">
                    <form.Field name="enabled">
                      {(field) => (
                        <Switch
                          checked={field.state.value}
                          onCheckedChange={field.handleChange}
                          disabled={formDisabled}
                          aria-label="Enable OTEL forwarding"
                        />
                      )}
                    </form.Field>
                  </RequireScope>
                </Stack>

                <div className="border-border border-t" />

                <form.Field name="endpointUrl">
                  {(field) => (
                    <Stack gap={2}>
                      <Label htmlFor={field.name}>Endpoint URL</Label>
                      <Input
                        id={field.name}
                        name={field.name}
                        placeholder="https://collector.example.com"
                        value={field.state.value}
                        onChange={field.handleChange}
                        onBlur={field.handleBlur}
                        disabled={formDisabled}
                      />
                    </Stack>
                  )}
                </form.Field>

                <div className="border-border border-t" />

                <form.Field name="headers" mode="array">
                  {(headersField) => (
                    <Stack gap={2}>
                      <Stack
                        direction="horizontal"
                        justify="space-between"
                        align="center"
                      >
                        <Label>Headers</Label>
                        <RequireScope scope="org:admin" level="component">
                          <Button
                            type="button"
                            variant="secondary"
                            size="sm"
                            onClick={() => headersField.pushValue(blankRow())}
                            disabled={formDisabled}
                          >
                            <Plus className="mr-1 h-3.5 w-3.5" />
                            Add header
                          </Button>
                        </RequireScope>
                      </Stack>

                      {headersField.state.value.length === 0 ? (
                        <Text
                          variant="body"
                          className="text-muted-foreground text-sm"
                        >
                          No headers. Add any required authorization headers
                          (e.g.
                          <code className="bg-muted ml-1 px-1">
                            Authorization
                          </code>
                          ).
                        </Text>
                      ) : (
                        <Stack gap={2}>
                          {headersField.state.value.map((header, idx) => (
                            <form.Field
                              key={header.rowID}
                              name={`headers[${idx}].name` as const}
                            >
                              {(nameField) => (
                                <form.Field
                                  name={`headers[${idx}].value` as const}
                                >
                                  {(valueField) => (
                                    <HeaderRow
                                      name={nameField.state.value}
                                      value={valueField.state.value}
                                      hasStoredValue={preservesStoredValue(
                                        header,
                                      )}
                                      nameInputName={nameField.name}
                                      valueInputName={valueField.name}
                                      disabled={formDisabled}
                                      onNameChange={nameField.handleChange}
                                      onNameBlur={nameField.handleBlur}
                                      onValueChange={valueField.handleChange}
                                      onValueBlur={valueField.handleBlur}
                                      onRemove={() =>
                                        headersField.removeValue(idx)
                                      }
                                    />
                                  )}
                                </form.Field>
                              )}
                            </form.Field>
                          ))}
                        </Stack>
                      )}
                    </Stack>
                  )}
                </form.Field>

                <div className="border-border border-t" />

                <Stack
                  direction="horizontal"
                  justify="space-between"
                  align="center"
                >
                  <RequireScope scope="org:admin" level="component">
                    <Button
                      type="button"
                      variant="destructive-secondary"
                      size="sm"
                      onClick={() => void handleDelete()}
                      disabled={!isConfigured || formDisabled}
                    >
                      <Trash2 className="mr-1 h-3.5 w-3.5" />
                      Delete
                    </Button>
                  </RequireScope>
                  <RequireScope scope="org:admin" level="component">
                    <Button type="submit" disabled={!canSave}>
                      Save
                    </Button>
                  </RequireScope>
                </Stack>
              </fieldset>
            );
          }}
        </form.Subscribe>
      </form>
    </Stack>
  );
}

function HeaderRow({
  name,
  value,
  hasStoredValue,
  nameInputName,
  valueInputName,
  disabled,
  onNameChange,
  onNameBlur,
  onValueChange,
  onValueBlur,
  onRemove,
}: {
  name: string;
  value: string;
  hasStoredValue: boolean;
  nameInputName: string;
  valueInputName: string;
  disabled: boolean;
  onNameChange: (value: string) => void;
  onNameBlur: () => void;
  onValueChange: (value: string) => void;
  onValueBlur: () => void;
  onRemove: () => void;
}) {
  return (
    <Stack direction="horizontal" gap={2} align="center">
      <Input
        name={nameInputName}
        placeholder="Header name"
        value={name}
        onChange={onNameChange}
        onBlur={onNameBlur}
        disabled={disabled}
        className="flex-1"
      />
      <Input
        name={valueInputName}
        placeholder={hasStoredValue ? "••••" : "Header value"}
        value={value}
        onChange={onValueChange}
        onBlur={onValueBlur}
        type="password"
        disabled={disabled}
        className="flex-1"
      />
      <Button
        type="button"
        variant="tertiary"
        size="sm"
        onClick={onRemove}
        disabled={disabled}
        aria-label={`Remove header ${name || "row"}`}
      >
        <Trash2 className="h-3.5 w-3.5" />
      </Button>
    </Stack>
  );
}
