import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import {
  invalidateAllCursorIntegrationConfig,
  useCursorIntegrationConfig,
} from "@gram/client/react-query/cursorIntegrationConfig";
import { useDeleteCursorIntegrationConfigMutation } from "@gram/client/react-query/deleteCursorIntegrationConfig";
import { useUpsertCursorIntegrationConfigMutation } from "@gram/client/react-query/upsertCursorIntegrationConfig";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { KeyRound, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

export function CursorIntegrationSection() {
  const { data, isLoading } = useCursorIntegrationConfig();
  const queryClient = useQueryClient();
  const { mutate: upsert, status: upsertStatus } =
    useUpsertCursorIntegrationConfigMutation({
      onSuccess: () => {
        toast.success("Cursor integration saved");
        setApiKey("");
        void invalidateAllCursorIntegrationConfig(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to save Cursor integration: ${err.message}`);
      },
    });
  const { mutate: deleteConfig, status: deleteStatus } =
    useDeleteCursorIntegrationConfigMutation({
      onSuccess: () => {
        toast.success("Cursor integration deleted");
        void invalidateAllCursorIntegrationConfig(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to delete Cursor integration: ${err.message}`);
      },
    });

  const [enabled, setEnabled] = useState(false);
  const [apiKey, setApiKey] = useState("");

  const isConfigured = Boolean(data?.id);
  const hasSavedKey = Boolean(data?.hasApiKey);

  useEffect(() => {
    if (!data) return;
    setEnabled(data.enabled);
    setApiKey("");
  }, [data]);

  const isMutating = upsertStatus === "pending" || deleteStatus === "pending";
  const canSave = useMemo(
    () => (apiKey.trim() !== "" || hasSavedKey) && !isMutating,
    [apiKey, hasSavedKey, isMutating],
  );

  const handleSave = () => {
    upsert({
      request: {
        upsertConfigRequestBody: {
          apiKey: apiKey.trim(),
          enabled,
        },
      },
    });
  };

  const handleDelete = () => {
    if (!isConfigured) return;
    if (!window.confirm("Delete this Cursor integration?")) return;
    deleteConfig({ request: {} });
    setEnabled(false);
    setApiKey("");
  };

  return (
    <Stack gap={4}>
      <div>
        <Heading variant="h4" className="mb-2">
          Cursor integration
        </Heading>
        <Type muted small>
          Poll Cursor Admin API usage events hourly for this project so token
          usage and cost are available in logs and analytics. API keys are
          encrypted at rest and never returned by the API.
        </Type>
      </div>

      <div className="border-border bg-card flex flex-col gap-4 rounded-lg border p-4">
        <Stack direction="horizontal" justify="space-between" align="center">
          <Stack gap={1}>
            <Stack direction="horizontal" align="center" gap={2}>
              <KeyRound className="text-muted-foreground h-4 w-4" />
              <Type variant="body" className="font-medium">
                Enable Cursor usage polling
              </Type>
            </Stack>
            <Type variant="body" className="text-muted-foreground ml-6 text-sm">
              Fetch hourly token and cost metrics from Cursor for this project.
            </Type>
          </Stack>
          <RequireScope scope="project:write" level="component">
            <Switch
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={isLoading || isMutating}
              aria-label="Enable Cursor usage polling"
            />
          </RequireScope>
        </Stack>

        <div className="border-border border-t" />

        <Stack gap={2}>
          <Label htmlFor="cursor-api-key">Cursor API key</Label>
          <Input
            id="cursor-api-key"
            placeholder={hasSavedKey ? "•••••• (saved)" : "key_xxx"}
            value={apiKey}
            onChange={setApiKey}
            type="password"
            disabled={isLoading || isMutating}
          />
          {data?.lastPolledAt ? (
            <Type variant="body" className="text-muted-foreground text-sm">
              Last polled {data.lastPolledAt.toLocaleString()}.
            </Type>
          ) : null}
        </Stack>

        <div className="border-border border-t" />

        <Stack direction="horizontal" justify="space-between" align="center">
          <RequireScope scope="project:write" level="component">
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
          <RequireScope scope="project:write" level="component">
            <Button onClick={handleSave} disabled={!canSave}>
              Save
            </Button>
          </RequireScope>
        </Stack>
      </div>
    </Stack>
  );
}
