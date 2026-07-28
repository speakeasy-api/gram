import { SimpleTooltip } from "@/components/ui/tooltip";
import { Badge } from "@speakeasy-api/moonshine";
import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  Loader2,
  PauseCircle,
} from "lucide-react";
import type { ScheduleRuntime } from "./use-ai-integration-schedules";

// Status badge for one stream, keyed off the real per-schedule scheduler
// state from aiIntegrations.listSchedules.
export function ScheduleStatusBadge({
  runtime,
  configured,
  connectionEnabled,
}: {
  runtime: ScheduleRuntime;
  configured: boolean;
  connectionEnabled: boolean;
}): JSX.Element {
  const status = getScheduleStatus({ runtime, configured, connectionEnabled });
  const Icon = iconForStatus(status.icon);
  const iconClassName =
    status.icon === "syncing" ? "h-3.5 w-3.5 animate-spin" : "h-3.5 w-3.5";
  return (
    <SimpleTooltip tooltip={status.detail ?? status.label}>
      <Badge variant={status.variant} background className="shrink-0">
        <Badge.LeftIcon>
          <Icon className={iconClassName} />
        </Badge.LeftIcon>
        <Badge.Text>{status.label}</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

type ScheduleStatus = {
  label: string;
  variant: "destructive" | "neutral" | "success" | "warning";
  icon: "failed" | "paused" | "pending" | "success" | "syncing";
  detail?: string;
};

function getScheduleStatus({
  runtime,
  configured,
  connectionEnabled,
}: {
  runtime: ScheduleRuntime;
  configured: boolean;
  connectionEnabled: boolean;
}): ScheduleStatus {
  if (!configured) {
    return {
      label: "Not connected",
      variant: "neutral",
      icon: "paused",
      detail: "Connect the provider before this stream can run.",
    };
  }
  if (runtime.isMutating) {
    return {
      label: "Updating",
      variant: "warning",
      icon: "syncing",
      detail: "Applying your change.",
    };
  }
  if (!connectionEnabled) {
    return {
      label: "Disabled",
      variant: "neutral",
      icon: "paused",
      detail: "The provider connection is off, so this stream will not poll.",
    };
  }
  if (!runtime.enabled) {
    return {
      label: "Paused",
      variant: "neutral",
      icon: "paused",
      detail: "This stream is paused. Turn it on to resume polling.",
    };
  }
  if (runtime.status === "auto_paused") {
    return {
      label: "Auto-paused",
      variant: "destructive",
      icon: "paused",
      detail:
        runtime.error ??
        "Paused automatically after repeated provider rejections. Fix the credentials or retry to resume.",
    };
  }
  if (runtime.status === "failed") {
    return {
      label: "Degraded",
      variant: "destructive",
      icon: "failed",
      detail: runtime.error ?? "The last sync failed. We retry automatically.",
    };
  }
  if (runtime.status === "success") {
    return {
      label: "Healthy",
      variant: "success",
      icon: "success",
      detail: runtime.lastSyncedAt
        ? `Last synced ${runtime.lastSyncedAt.toLocaleString()}.`
        : undefined,
    };
  }
  return {
    label: "Pending",
    variant: "warning",
    icon: "pending",
    detail: "Waiting for the first sync.",
  };
}

function iconForStatus(status: ScheduleStatus["icon"]) {
  switch (status) {
    case "failed":
      return AlertCircle;
    case "paused":
      return PauseCircle;
    case "success":
      return CheckCircle2;
    case "pending":
      return Clock3;
    case "syncing":
      return Loader2;
  }
}
