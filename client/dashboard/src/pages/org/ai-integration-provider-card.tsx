import { RequireScope } from "@/components/require-scope";
import { Card } from "@/components/ui/card";
import { useConfirm } from "@/components/ui/use-confirm";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useAiIntegrationConfig } from "@gram/client/react-query/aiIntegrationConfig";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  TimerReset,
  Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import type { AIIntegrationProvider } from "./ai-integration-providers";
import { useAIIntegrationConfigForm } from "./use-ai-integration-config-form";

export function AIIntegrationProviderCard({
  provider,
}: {
  provider: AIIntegrationProvider;
}): JSX.Element {
  const form = useAIIntegrationConfigForm(provider);
  const Icon = provider.icon;
  const apiKeyFieldId = `${provider.provider}-ai-integration-api-key`;
  const orgIdFieldId = `${provider.provider}-ai-integration-org-id`;
  const billingModeFieldId = `${provider.provider}-ai-integration-billing-mode`;
  const { confirm: requestConfirm, dialog } = useConfirm();

  const handleDelete = async () => {
    if (!form.isConfigured) return;
    const confirmed = await requestConfirm({
      title: `Delete the ${provider.name} AI integration?`,
      destructive: true,
    });
    if (!confirmed) return;
    form.remove();
  };

  return (
    <Card className="gap-4">
      <Stack direction="horizontal" justify="space-between" align="center">
        <Stack gap={1}>
          <Stack direction="horizontal" align="center" gap={2}>
            <Icon className="text-foreground h-4 w-4" />
            <Type variant="body" className="font-medium">
              {provider.name}
            </Type>
            <PollStatusIcon config={form.data} label={provider.name} />
            <NextPollBadge config={form.data} />
          </Stack>
          <Type variant="body" className="text-muted-foreground ml-6 text-sm">
            {provider.description}
          </Type>
        </Stack>
        <RequireScope scope="org:admin" level="component">
          <Switch
            checked={form.enabled}
            onCheckedChange={form.setEnabled}
            disabled={form.isLoading || form.isMutating}
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
            form.hasSavedKey ? "•••••• (saved)" : provider.apiKeyPlaceholder
          }
          value={form.apiKey}
          onChange={(e) => form.setApiKey(e.target.value)}
          type="password"
          disabled={form.isLoading || form.isMutating}
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
            value={form.organizationId}
            onChange={(e) => form.setOrganizationId(e.target.value)}
            disabled={form.isLoading || form.isMutating}
          />
        </Stack>
      ) : null}

      <Stack gap={2}>
        <Label htmlFor={billingModeFieldId}>Billing mode</Label>
        <Select
          value={form.billingMode || "unknown"}
          onValueChange={(value) => form.setBillingMode(value)}
          disabled={form.isLoading || form.isMutating}
        >
          <SelectTrigger id={billingModeFieldId}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="unknown">Unknown</SelectItem>
            <SelectItem value="metered">Metered (pay-per-token)</SelectItem>
            <SelectItem value="flat_rate">
              Flat rate (subscription seats)
            </SelectItem>
          </SelectContent>
        </Select>
        <Type variant="body" className="text-muted-foreground text-xs">
          Dashboard cost is estimated from token usage at API rates. Only
          "Metered" accounts are billed per token, so their cost is shown as
          real spend; subscription plans show it as an estimate.
        </Type>
      </Stack>

      <div className="border-border border-t" />

      <Stack direction="horizontal" justify="space-between" align="center">
        <RequireScope scope="org:admin" level="component">
          <Button
            variant="destructive-primary"
            size="sm"
            onClick={() => void handleDelete()}
            disabled={!form.isConfigured || form.isMutating}
          >
            <Button.LeftIcon>
              <Trash2 className="h-3.5 w-3.5" />
            </Button.LeftIcon>
            <Button.Text>Delete</Button.Text>
          </Button>
        </RequireScope>
        <RequireScope scope="org:admin" level="component">
          <Button onClick={form.save} disabled={!form.canSave}>
            Save
          </Button>
        </RequireScope>
      </Stack>
      {dialog}
    </Card>
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
