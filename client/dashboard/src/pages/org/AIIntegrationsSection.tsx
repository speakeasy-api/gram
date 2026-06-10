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
import { ReactNode, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { ClaudeCodeIcon, CursorIcon } from "../hooks/HookSourceIcon";

type ProviderIcon = (props: { className?: string }) => JSX.Element;

type AIIntegrationProvider = {
  provider: string;
  name: string;
  description: string;
  icon: ProviderIcon;
  apiKeyLabel: string;
  apiKeyPlaceholder: string;
  requiresOrganizationId: boolean;
  organizationIdLabel?: string;
  organizationIdPlaceholder?: string;
  helpText?: ReactNode;
};

const PROVIDERS: AIIntegrationProvider[] = [
  {
    provider: "cursor",
    name: "Cursor",
    description: "Track Cursor usage and spend across your organization.",
    icon: CursorIcon,
    apiKeyLabel: "Cursor API key",
    apiKeyPlaceholder: "key_xxx",
    requiresOrganizationId: false,
  },
  {
    provider: "anthropic_compliance",
    name: "Anthropic Compliance",
    description:
      "Import Claude.ai chat transcripts and files for compliance review.",
    icon: ClaudeCodeIcon,
    apiKeyLabel: "Anthropic API key",
    apiKeyPlaceholder: "sk-ant-admin...",
    requiresOrganizationId: true,
    organizationIdLabel: "Anthropic organization ID",
    organizationIdPlaceholder: "org_xxx",
    helpText: (
      <>
        The API key must include the{" "}
        <code className="text-foreground">read:compliance_activities</code> and{" "}
        <code className="text-foreground">read:compliance_user_data</code>{" "}
        scopes.
      </>
    ),
  },
];

export function AIIntegrationsSection(): JSX.Element {
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

      {PROVIDERS.map((provider) => (
        <AIIntegrationProviderCard
          key={provider.provider}
          provider={provider}
        />
      ))}
    </Stack>
  );
}

function AIIntegrationProviderCard({
  provider,
}: {
  provider: AIIntegrationProvider;
}): JSX.Element {
  const { data, isLoading } = useAiIntegrationConfig({
    provider: provider.provider,
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
  const [organizationId, setOrganizationId] = useState("");

  const isConfigured = Boolean(data?.id);
  const hasSavedKey = Boolean(data?.hasApiKey);

  // Sync form state from the persisted config. Depend on primitive values
  // rather than `data` itself: refetches produce new object references even
  // when nothing changed, and resetting on every refetch would discard
  // unsaved edits. The config id is included so that loading a *different*
  // config record (e.g. after the active organization changes) resets the
  // form even when the saved values happen to match the previous config.
  const hasData = Boolean(data);
  const savedId = data?.id ?? "";
  const savedEnabled = data?.enabled ?? false;
  const savedOrganizationId = data?.externalOrganizationId ?? "";

  useEffect(() => {
    if (!hasData) return;
    setEnabled(savedEnabled);
    setApiKey("");
    setOrganizationId(savedOrganizationId);
  }, [hasData, savedId, savedEnabled, savedOrganizationId]);

  const Icon = provider.icon;
  const apiKeyFieldId = `${provider.provider}-ai-integration-api-key`;
  const orgIdFieldId = `${provider.provider}-ai-integration-org-id`;

  const isMutating = upsertStatus === "pending" || deleteStatus === "pending";
  const canSave = useMemo(() => {
    if (isMutating) return false;
    if (apiKey.trim() === "" && !hasSavedKey) return false;
    if (provider.requiresOrganizationId && organizationId.trim() === "") {
      return false;
    }
    return true;
  }, [
    apiKey,
    hasSavedKey,
    isMutating,
    organizationId,
    provider.requiresOrganizationId,
  ]);

  const handleSave = () => {
    upsert({
      request: {
        upsertAIIntegrationConfigRequest: {
          provider: provider.provider,
          apiKey: apiKey.trim(),
          enabled,
          ...(provider.requiresOrganizationId
            ? { externalOrganizationId: organizationId.trim() }
            : {}),
        },
      },
    });
  };

  const handleDelete = () => {
    if (!isConfigured) return;
    if (!window.confirm(`Delete the ${provider.name} AI integration?`)) return;
    deleteConfig({
      request: {
        deleteAIIntegrationConfigRequest: {
          provider: provider.provider,
        },
      },
    });
    setEnabled(false);
    setApiKey("");
    setOrganizationId("");
  };

  return (
    <div className="border-border bg-card flex flex-col gap-4 rounded-lg border p-4">
      <Stack direction="horizontal" justify="space-between" align="center">
        <Stack gap={1}>
          <Stack direction="horizontal" align="center" gap={2}>
            <Icon className="text-foreground h-4 w-4" />
            <Type variant="body" className="font-medium">
              {provider.name}
            </Type>
            <PollStatusIcon config={data} label={provider.name} />
            <NextPollBadge config={data} />
          </Stack>
          <Type variant="body" className="text-muted-foreground ml-6 text-sm">
            {provider.description}
          </Type>
        </Stack>
        <RequireScope scope="org:admin" level="component">
          <Switch
            checked={enabled}
            onCheckedChange={setEnabled}
            disabled={isLoading || isMutating}
            aria-label={`Enable ${provider.name} AI integration`}
          />
        </RequireScope>
      </Stack>

      <div className="border-border border-t" />

      <Stack gap={2}>
        <Label htmlFor={apiKeyFieldId}>{provider.apiKeyLabel}</Label>
        <Input
          id={apiKeyFieldId}
          placeholder={
            hasSavedKey ? "•••••• (saved)" : provider.apiKeyPlaceholder
          }
          value={apiKey}
          onChange={setApiKey}
          type="password"
          disabled={isLoading || isMutating}
        />
        {provider.helpText ? (
          <Type variant="body" className="text-muted-foreground text-xs">
            {provider.helpText}
          </Type>
        ) : null}
      </Stack>

      {provider.requiresOrganizationId ? (
        <Stack gap={2}>
          <Label htmlFor={orgIdFieldId}>
            {provider.organizationIdLabel ?? "Organization ID"}
          </Label>
          <Input
            id={orgIdFieldId}
            placeholder={provider.organizationIdPlaceholder}
            value={organizationId}
            onChange={setOrganizationId}
            disabled={isLoading || isMutating}
          />
        </Stack>
      ) : null}

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
  );
}

function PollStatusIcon({
  config,
  label,
}: {
  config: ReturnType<typeof useAiIntegrationConfig>["data"];
  label: string;
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
        : `The latest ${label} sync failed.`,
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
      ? `${label} last synced ${config.lastPolledAt.toLocaleString()}.`
      : `${label} sync succeeded.`;

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
    <SimpleTooltip tooltip={`Waiting for the first ${label} sync.`}>
      <Badge variant="warning" background className="shrink-0">
        <Badge.LeftIcon>
          <Clock3 className="h-3.5 w-3.5" />
        </Badge.LeftIcon>
        <Badge.Text>Pending</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

function NextPollBadge({
  config,
}: {
  config: ReturnType<typeof useAiIntegrationConfig>["data"];
}) {
  const [now, setNow] = useState(() => new Date());
  const nextPollAt = getNextPollAt(config);
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

function getNextPollAt(
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
