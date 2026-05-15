import { Button } from "@/components/ui/button";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import {
  invalidateAllOtelForwardingConfig,
  useOtelForwardingConfig,
} from "@gram/client/react-query/otelForwardingConfig";
import { useUpsertOtelForwardingConfigMutation } from "@gram/client/react-query/upsertOtelForwardingConfig";
import { useDeleteOtelForwardingConfigMutation } from "@gram/client/react-query/deleteOtelForwardingConfig";
import type { OtelForwardingHeader } from "@gram/client/models/components";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Plus, Send, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

type EditableHeader = {
  // Local-only id; stable while a row is mounted so we can edit a name in
  // place without unmounting the row.
  rowID: string;
  name: string;
  // Empty string with hasStoredValue=true means "keep existing encrypted
  // value." A non-empty string overwrites it. A row whose hasStoredValue is
  // false and value is empty is a brand-new blank row.
  value: string;
  hasStoredValue: boolean;
};

function rowFromServer(h: OtelForwardingHeader, idx: number): EditableHeader {
  return {
    rowID: `existing-${idx}-${h.name}`,
    name: h.name,
    value: "",
    hasStoredValue: h.hasValue,
  };
}

let newRowCounter = 0;
function blankRow(): EditableHeader {
  newRowCounter += 1;
  return {
    rowID: `new-${newRowCounter}`,
    name: "",
    value: "",
    hasStoredValue: false,
  };
}

export function OtelForwardingSection() {
  const { data, isLoading } = useOtelForwardingConfig();
  const queryClient = useQueryClient();
  const { mutate: upsert, status: upsertStatus } =
    useUpsertOtelForwardingConfigMutation({
      onSuccess: () => {
        toast.success("Forwarding config saved");
        void invalidateAllOtelForwardingConfig(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to save forwarding config: ${err.message}`);
      },
    });
  const { mutate: deleteConfig, status: deleteStatus } =
    useDeleteOtelForwardingConfigMutation({
      onSuccess: () => {
        toast.success("Forwarding config deleted");
        void invalidateAllOtelForwardingConfig(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to delete forwarding config: ${err.message}`);
      },
    });

  const [enabled, setEnabled] = useState(false);
  const [url, setUrl] = useState("");
  const [headers, setHeaders] = useState<EditableHeader[]>([]);

  const isConfigured = Boolean(data?.id);

  // Pull server values into the form on first load and whenever the server
  // copy changes (e.g. after another tab edits the config).
  useEffect(() => {
    if (!data) return;
    setEnabled(data.enabled);
    setUrl(data.endpointUrl);
    setHeaders(data.headers.map(rowFromServer));
  }, [data]);

  const isMutating = upsertStatus === "pending" || deleteStatus === "pending";

  const trimmedUrl = url.trim();
  const hasValidHeaders = useMemo(
    () =>
      headers.every((h) => {
        const name = h.name.trim();
        if (name === "") return false;
        // Allow keeping an existing value (hasStoredValue + empty input).
        if (h.value === "" && !h.hasStoredValue) return false;
        return true;
      }),
    [headers],
  );
  const canSave = trimmedUrl !== "" && hasValidHeaders && !isMutating;

  const handleSave = () => {
    upsert({
      request: {
        upsertConfigRequestBody: {
          endpointUrl: trimmedUrl,
          enabled,
          headers: headers
            .filter((h) => h.name.trim() !== "")
            .map((h) => ({
              name: h.name.trim(),
              // Server treats empty + previously-stored as a no-op: it will
              // re-encrypt the existing value. Sending the new value when
              // the user typed one in.
              value: h.value,
            })),
        },
      },
    });
  };

  const handleDelete = () => {
    if (!isConfigured) return;
    deleteConfig({ request: {} });
    setEnabled(false);
    setUrl("");
    setHeaders([]);
  };

  return (
    <Stack gap={4}>
      <div>
        <Heading variant="h4" className="mb-2">
          OTEL forwarding
        </Heading>
        <Type muted small>
          Forward a copy of every OTEL payload received on the hooks endpoint to
          your own collector. Headers are encrypted at rest; values are never
          returned by the API.
        </Type>
      </div>

      <div className="border-border bg-card flex flex-col gap-4 rounded-lg border p-4">
        <Stack direction="horizontal" justify="space-between" align="center">
          <Stack gap={1}>
            <Stack direction="horizontal" align="center" gap={2}>
              <Send className="text-muted-foreground h-4 w-4" />
              <Type variant="body" className="font-medium">
                Enable forwarding
              </Type>
            </Stack>
            <Type variant="body" className="text-muted-foreground ml-6 text-sm">
              Send each inbound OTEL payload to the endpoint below.
            </Type>
          </Stack>
          <RequireScope scope="org:admin" level="component">
            <Switch
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={isLoading || isMutating}
              aria-label="Enable OTEL forwarding"
            />
          </RequireScope>
        </Stack>

        <div className="border-border border-t" />

        <Stack gap={2}>
          <Label htmlFor="otel-forwarding-url">Endpoint URL</Label>
          <Input
            id="otel-forwarding-url"
            placeholder="https://collector.example.com"
            value={url}
            onChange={setUrl}
            disabled={isLoading || isMutating}
          />
        </Stack>

        <div className="border-border border-t" />

        <Stack gap={2}>
          <Stack direction="horizontal" justify="space-between" align="center">
            <Label>Headers</Label>
            <RequireScope scope="org:admin" level="component">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setHeaders((prev) => [...prev, blankRow()])}
                disabled={isLoading || isMutating}
              >
                <Plus className="mr-1 h-3.5 w-3.5" />
                Add header
              </Button>
            </RequireScope>
          </Stack>

          {headers.length === 0 ? (
            <Type variant="body" className="text-muted-foreground text-sm">
              No headers. Add any required authorization headers (e.g.
              <code className="bg-muted ml-1 rounded px-1">Authorization</code>
              ).
            </Type>
          ) : (
            <Stack gap={2}>
              {headers.map((header, idx) => (
                <HeaderRow
                  key={header.rowID}
                  header={header}
                  disabled={isLoading || isMutating}
                  onChange={(next) =>
                    setHeaders((prev) => {
                      const copy = prev.slice();
                      copy[idx] = next;
                      return copy;
                    })
                  }
                  onRemove={() =>
                    setHeaders((prev) => prev.filter((_, i) => i !== idx))
                  }
                />
              ))}
            </Stack>
          )}
        </Stack>

        <div className="border-border border-t" />

        <Stack direction="horizontal" justify="space-between" align="center">
          <RequireScope scope="org:admin" level="component">
            <Button
              variant="destructive"
              size="sm"
              onClick={handleDelete}
              disabled={!isConfigured || isMutating}
            >
              <Trash2 className="mr-1 h-3.5 w-3.5" />
              Delete
            </Button>
          </RequireScope>
          <RequireScope scope="org:admin" level="component">
            <Button onClick={handleSave} disabled={!canSave}>
              Save
            </Button>
          </RequireScope>
        </Stack>
      </div>
    </Stack>
  );
}

function HeaderRow({
  header,
  disabled,
  onChange,
  onRemove,
}: {
  header: EditableHeader;
  disabled: boolean;
  onChange: (next: EditableHeader) => void;
  onRemove: () => void;
}) {
  return (
    <Stack direction="horizontal" gap={2} align="center">
      <Input
        placeholder="Header name"
        value={header.name}
        onChange={(value) => onChange({ ...header, name: value })}
        disabled={disabled}
        className="flex-1"
      />
      <Input
        placeholder={header.hasStoredValue ? "•••••• (saved)" : "Header value"}
        value={header.value}
        onChange={(value) => onChange({ ...header, value })}
        type="password"
        disabled={disabled}
        className="flex-1"
      />
      <Button
        variant="ghost"
        size="sm"
        onClick={onRemove}
        disabled={disabled}
        aria-label={`Remove header ${header.name || "row"}`}
      >
        <Trash2 className="h-3.5 w-3.5" />
      </Button>
    </Stack>
  );
}
