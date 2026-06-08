import { RequireScope } from "@/components/require-scope";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Button } from "@/components/ui/button";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import {
  invalidateAllAiIntegrationConfig,
  useAiIntegrationConfig,
} from "@gram/client/react-query/aiIntegrationConfig";
import { useDeleteAIIntegrationConfigMutation } from "@gram/client/react-query/deleteAIIntegrationConfig";
import { useUpsertAIIntegrationConfigMutation } from "@gram/client/react-query/upsertAIIntegrationConfig";
import { Badge, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  TimerReset,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { CursorIcon } from "../hooks/HookSourceIcon";

const CURSOR_PROVIDER = "cursor";

export function AIIntegrationsSection(): JSX.Element {
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
        upsertAIIntegrationConfigRequest: {
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
        deleteAIIntegrationConfigRequest: {
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
        <Stack direction="horizontal" gap={2} align="center" className="mb-2">
          <Heading variant="h4">AI Integrations</Heading>
          <ReleaseStageBadge stage="preview" />
        </Stack>
        <Type muted small>
          Connect AI providers for usage and cost reporting, with room for more
          use cases as providers expose more data.
        </Type>
      </div>

      <div className="border-border bg-card flex flex-col gap-4 rounded-lg border p-4">
        <Stack direction="horizontal" justify="space-between" align="center">
          <Stack gap={1}>
            <Stack direction="horizontal" align="center" gap={2}>
              <CursorIcon className="text-foreground h-4 w-4" />
              <Type variant="body" className="font-medium">
                Cursor
              </Type>
              <CursorPollStatusIcon config={data} />
              <CursorNextPollBadge config={data} />
            </Stack>
            <Type variant="body" className="text-muted-foreground ml-6 text-sm">
              Track Cursor usage and spend across your organization.
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

function CursorPollStatusIcon({
  config,
}: {
  config: ReturnType<typeof useAiIntegrationConfig>["data"];
}) {
  if (!config?.id || !config.lastPollStatus) {
    return null;
  }
  if (!config.enabled && config.lastPollStatus === "pending") {
    return null;
  }

  if (config.lastPollStatus === "failed") {
    const tooltip = [
      config.lastPollFailedAt
        ? `Failed at ${config.lastPollFailedAt.toLocaleString()}.`
        : "The latest Cursor usage sync failed.",
      `Error: ${config.lastPollError ?? "We will retry automatically."}`,
    ].join(" ");

    return (
      <SimpleTooltip tooltip={tooltip}>
        <Badge variant="destructive" background className="shrink-0">
          <Badge.LeftIcon>
            <AlertCircle className="h-3.5 w-3.5" />
          </Badge.LeftIcon>
          <Badge.Text>Sync failed</Badge.Text>
        </Badge>
      </SimpleTooltip>
    );
  }

  if (config.lastPollStatus === "success") {
    const tooltip = config.lastPolledAt
      ? `Cursor usage last synced ${config.lastPolledAt.toLocaleString()}.`
      : "Cursor usage sync succeeded.";

    return (
      <SimpleTooltip tooltip={tooltip}>
        <Badge variant="success" background className="shrink-0">
          <Badge.LeftIcon>
            <CheckCircle2 className="h-3.5 w-3.5" />
          </Badge.LeftIcon>
          <Badge.Text>Synced</Badge.Text>
        </Badge>
      </SimpleTooltip>
    );
  }

  return (
    <SimpleTooltip tooltip="Waiting for the first Cursor usage sync.">
      <Badge variant="warning" background className="shrink-0">
        <Badge.LeftIcon>
          <Clock3 className="h-3.5 w-3.5" />
        </Badge.LeftIcon>
        <Badge.Text>Pending</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

function CursorNextPollBadge({
  config,
}: {
  config: ReturnType<typeof useAiIntegrationConfig>["data"];
}) {
  const [now, setNow] = useState(() => new Date());
  const nextPollAt = getCursorNextPollAt(config);
  const nextPollAtTime = nextPollAt?.getTime();

  useEffect(() => {
    if (!nextPollAtTime) return;
    const interval = window.setInterval(() => setNow(new Date()), 60_000);
    return () => window.clearInterval(interval);
  }, [nextPollAtTime]);

  if (!config?.id || !config.enabled || !nextPollAt) {
    return null;
  }

  return (
    <Badge variant="neutral" background className="shrink-0">
      <Badge.LeftIcon>
        <TimerReset className="h-3.5 w-3.5" />
      </Badge.LeftIcon>
      <Badge.Text>{formatNextPollLabel(nextPollAt, now)}</Badge.Text>
    </Badge>
  );
}

function getCursorNextPollAt(
  config: ReturnType<typeof useAiIntegrationConfig>["data"],
) {
  if (config?.nextPollAfter) {
    return config.nextPollAfter;
  }
  if (config?.lastPolledAt) {
    return addHours(config.lastPolledAt, 1);
  }
  if (config?.lastPollFailedAt) {
    return addHours(config.lastPollFailedAt, 1);
  }
  if (config?.lastPollStatus === "pending") {
    return new Date();
  }
  return null;
}

function addHours(date: Date, hours: number) {
  return new Date(date.getTime() + hours * 60 * 60 * 1000);
}

function formatNextPollLabel(nextPollAfter: Date, now: Date) {
  const remainingMs = nextPollAfter.getTime() - now.getTime();
  if (remainingMs <= 0) {
    return "Poll due";
  }

  const remainingMinutes = Math.max(1, Math.ceil(remainingMs / 60_000));
  if (remainingMinutes < 60) {
    return `Next poll in ${remainingMinutes}m`;
  }

  const hours = Math.floor(remainingMinutes / 60);
  const minutes = remainingMinutes % 60;
  if (minutes === 0) {
    return `Next poll in ${hours}h`;
  }
  return `Next poll in ${hours}h ${minutes}m`;
}
