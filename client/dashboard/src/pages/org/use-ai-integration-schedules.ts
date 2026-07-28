import {
  invalidateAiIntegrationSchedules,
  useAiIntegrationSchedules,
} from "@gram/client/react-query/aiIntegrationSchedules";
import { useRetryAIIntegrationScheduleMutation } from "@gram/client/react-query/retryAIIntegrationSchedule";
import { useSetAIIntegrationScheduleEnabledMutation } from "@gram/client/react-query/setAIIntegrationScheduleEnabled";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import type { AIIntegrationProvider } from "./ai-integration-providers";

type ScheduleRuntimeStatus =
  | "pending"
  | "success"
  | "failed"
  | "auto_paused"
  | "disabled";

// Per-schedule scheduler state as the UI consumes it, mapped from the
// aiIntegrations.listSchedules endpoint.
export type ScheduleRuntime = {
  enabled: boolean;
  status: ScheduleRuntimeStatus;
  lastSyncedAt: Date | null;
  error: string | null;
  isMutating: boolean;
  // Product-level stream identifier and kind, owned by the backend registry
  // (streamForSchedule in server/internal/aiintegrations). Null until the
  // backend has a sync row for the schedule; display falls back to the static
  // provider metadata in ai-integration-providers.tsx.
  stream: string | null;
  streamKind: "events" | "metrics" | null;
};

type ScheduleRuntimeMap = Record<string, ScheduleRuntime>;

// Rendered for schedules the backend has no sync row for yet (unconfigured
// provider, or a config that predates the schedule).
const DEFAULT_RUNTIME: ScheduleRuntime = {
  enabled: true,
  status: "pending",
  lastSyncedAt: null,
  error: null,
  isMutating: false,
  stream: null,
  streamKind: null,
};

export type UseScheduleRuntimes = {
  runtimes: ScheduleRuntimeMap;
  isLoading: boolean;
  toggle: (schedule: string, enabled: boolean) => void;
  retry: (schedule: string) => void;
};

// Live per-schedule state plus instant, self-contained enable/disable and
// retry actions, independent of the credentials Save flow.
export function useScheduleRuntimes(
  provider: AIIntegrationProvider,
): UseScheduleRuntimes {
  const queryClient = useQueryClient();
  const { data, isLoading } = useAiIntegrationSchedules({
    provider: provider.provider,
  });
  const [inFlightSchedule, setInFlightSchedule] = useState<string | null>(null);

  const refresh = useCallback(() => {
    setInFlightSchedule(null);
    void invalidateAiIntegrationSchedules(queryClient, [
      { provider: provider.provider },
    ]);
  }, [queryClient, provider.provider]);

  const { mutate: mutateEnabled } = useSetAIIntegrationScheduleEnabledMutation({
    onSuccess: refresh,
    onError: (err) => {
      refresh();
      toast.error(`Failed to update stream: ${err.message}`);
    },
  });
  const { mutate: mutateRetry } = useRetryAIIntegrationScheduleMutation({
    onSuccess: () => {
      refresh();
      toast.success("Retry scheduled. The next poll runs within a minute.");
    },
    onError: (err) => {
      refresh();
      toast.error(`Failed to retry stream: ${err.message}`);
    },
  });

  const runtimes = useMemo(() => {
    const map: ScheduleRuntimeMap = {};
    for (const state of data?.schedules ?? []) {
      map[state.schedule] = {
        enabled: state.enabled,
        status: state.status,
        lastSyncedAt: state.lastPollSuccessAt ?? null,
        error: state.lastPollError ?? null,
        isMutating: state.schedule === inFlightSchedule,
        stream: state.stream ?? null,
        streamKind: state.streamKind ?? null,
      };
    }
    return map;
  }, [data, inFlightSchedule]);

  const toggle = useCallback(
    (schedule: string, enabled: boolean) => {
      setInFlightSchedule(schedule);
      mutateEnabled({
        request: {
          setScheduleEnabledRequestBody: {
            provider: provider.provider,
            schedule,
            enabled,
          },
        },
      });
    },
    [mutateEnabled, provider.provider],
  );

  const retry = useCallback(
    (schedule: string) => {
      setInFlightSchedule(schedule);
      mutateRetry({
        request: {
          retryScheduleRequestBody: {
            provider: provider.provider,
            schedule,
          },
        },
      });
    },
    [mutateRetry, provider.provider],
  );

  return { runtimes, isLoading, toggle, retry };
}

export function runtimeOrDefault(
  runtimes: ScheduleRuntimeMap,
  schedule: string,
): ScheduleRuntime {
  return runtimes[schedule] ?? DEFAULT_RUNTIME;
}

const MINUTE = 60_000;

export function formatRelativeTime(date: Date | null): string | null {
  if (!date) return null;
  const diffMs = Date.now() - date.getTime();
  const mins = Math.max(0, Math.round(diffMs / MINUTE));
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}
