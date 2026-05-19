import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/button";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import {
  invalidateAllAiIntegrationConfig,
  useAiIntegrationConfig,
} from "@gram/client/react-query/aiIntegrationConfig";
import { useDeleteAIIntegrationConfigMutation } from "@gram/client/react-query/deleteAIIntegrationConfig";
import { useUpsertAIIntegrationConfigMutation } from "@gram/client/react-query/upsertAIIntegrationConfig";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { KeyRound, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

const CURSOR_PROVIDER = "cursor";

export function AIIntegrationsSection() {
  const { data, isLoading } = useAiIntegrationConfig({
    provider: CURSOR_PROVIDER,
  });
  const queryClient = useQueryClient();
  const { mutate: upsert, status: upsertStatus } =
    useUpsertAIIntegrationConfigMutation({
      onSuccess: () => {
        toast.success("AI integration saved");
        setApiKey("");
        void invalidateAllAiIntegrationConfig(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to save AI integration: ${err.message}`);
      },
    });
  const { mutate: deleteConfig, status: deleteStatus } =
    useDeleteAIIntegrationConfigMutation({
      onSuccess: () => {
        toast.success("AI integration deleted");
        void invalidateAllAiIntegrationConfig(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to delete AI integration: ${err.message}`);
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
          provider: CURSOR_PROVIDER,
          apiKey: apiKey.trim(),
          enabled,
        },
      },
    });
  };

  const handleDelete = () => {
    if (!isConfigured) return;
    if (!window.confirm("Delete the Cursor AI integration?")) return;
    deleteConfig({
      request: {
        deleteConfigRequestBody: {
          provider: CURSOR_PROVIDER,
        },
      },
    });
    setEnabled(false);
    setApiKey("");
  };

  return (
    <Stack gap={4}>
      <div>
        <Heading variant="h4" className="mb-2">
          AI Integrations
        </Heading>
        <Type muted small>
          Connect AI providers to Gram. Provider integrations can power usage
          and cost reporting, with room for more use cases as providers expose
          more data.
        </Type>
      </div>

      <div className="border-border bg-card flex flex-col gap-4 rounded-lg border p-4">
        <Stack direction="horizontal" justify="space-between" align="center">
          <Stack gap={1}>
            <Stack direction="horizontal" align="center" gap={2}>
              <KeyRound className="text-muted-foreground h-4 w-4" />
              <Type variant="body" className="font-medium">
                Cursor
              </Type>
            </Stack>
            <Type variant="body" className="text-muted-foreground ml-6 text-sm">
              Connect a Cursor team Admin API key so Gram can sync hourly token
              and cost data. Synced usage is attributed to the
              organization&apos;s first-created project.
            </Type>
          </Stack>
          <RequireScope scope="org:admin" level="component">
            <Switch
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={isLoading || isMutating}
              aria-label="Enable Cursor AI integration"
            />
          </RequireScope>
        </Stack>

        <div className="border-border border-t" />

        <Stack gap={2}>
          <Label htmlFor="cursor-ai-integration-api-key">Cursor API key</Label>
          <Input
            id="cursor-ai-integration-api-key"
            placeholder={hasSavedKey ? "•••••• (saved)" : "key_xxx"}
            value={apiKey}
            onChange={setApiKey}
            type="password"
            disabled={isLoading || isMutating}
          />
          {data?.lastPolledAt ? (
            <Type variant="body" className="text-muted-foreground text-sm">
              Last synced {data.lastPolledAt.toLocaleString()}.
            </Type>
          ) : null}
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
