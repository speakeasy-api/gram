import type { ScheduleBadgeRuntime } from "@/components/schedule-status-badge";
import {
  invalidateDeviceIntegrationSchedules,
  useDeviceIntegrationSchedules,
} from "@gram/client/react-query/deviceIntegrationSchedules.js";
import { useRetryDeviceIntegrationScheduleMutation } from "@gram/client/react-query/retryDeviceIntegrationSchedule.js";
import { useSetDeviceIntegrationScheduleEnabledMutation } from "@gram/client/react-query/setDeviceIntegrationScheduleEnabled.js";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { type ProviderRole, ROLE_COPY } from "./provider-role";

export type ScheduleRuntime = ScheduleBadgeRuntime;

type ScheduleRuntimeMap = Record<string, ScheduleRuntime>;

// Rendered for schedules the backend has no sync row for yet (unconfigured
// provider, or a config that predates the schedule).
const DEFAULT_RUNTIME: ScheduleRuntime = {
  enabled: true,
  status: "pending",
  lastSyncedAt: null,
  error: null,
  isMutating: false,
};

export type UseScheduleRuntimes = {
  runtimes: ScheduleRuntimeMap;
  isLoading: boolean;
  toggle: (schedule: string, enabled: boolean) => void;
  retry: (schedule: string) => void;
};

// Live per-schedule state plus instant, self-contained enable/disable and
// retry actions, independent of the credentials Save flow.
export function useDeviceScheduleRuntimes(
  provider: string,
  // Controls the direction-specific "sync"/"push" vocabulary in result toasts.
  role: ProviderRole = "source",
): UseScheduleRuntimes {
  const queryClient = useQueryClient();
  const { data, isLoading } = useDeviceIntegrationSchedules({ provider });
  const [inFlightSchedule, setInFlightSchedule] = useState<string | null>(null);

  const refresh = useCallback(() => {
    setInFlightSchedule(null);
    void invalidateDeviceIntegrationSchedules(queryClient, [{ provider }]);
  }, [queryClient, provider]);

  const { mutate: mutateEnabled } =
    useSetDeviceIntegrationScheduleEnabledMutation({
      onSuccess: refresh,
      onError: (err) => {
        refresh();
        toast.error(`Failed to update schedule: ${err.message}`);
      },
    });
  const { mutate: mutateRetry } = useRetryDeviceIntegrationScheduleMutation({
    onSuccess: () => {
      refresh();
      toast.success(ROLE_COPY[role].retryStartedToast);
    },
    onError: (err) => {
      refresh();
      toast.error(`Failed to retry schedule: ${err.message}`);
    },
  });

  const runtimes = useMemo(() => {
    const map: ScheduleRuntimeMap = {};
    for (const state of data?.schedules ?? []) {
      map[state.schedule] = {
        enabled: state.enabled,
        status: state.status,
        lastSyncedAt: state.lastSyncSuccessAt ?? null,
        error: state.lastSyncError ?? null,
        isMutating: state.schedule === inFlightSchedule,
      };
    }
    return map;
  }, [data, inFlightSchedule]);

  const toggle = useCallback(
    (schedule: string, enabled: boolean) => {
      setInFlightSchedule(schedule);
      mutateEnabled({
        request: {
          setScheduleEnabledRequestBody: { provider, schedule, enabled },
        },
      });
    },
    [mutateEnabled, provider],
  );

  const retry = useCallback(
    (schedule: string) => {
      setInFlightSchedule(schedule);
      mutateRetry({
        request: {
          retryScheduleRequestBody: { provider, schedule },
        },
      });
    },
    [mutateRetry, provider],
  );

  return { runtimes, isLoading, toggle, retry };
}

export function runtimeOrDefault(
  runtimes: ScheduleRuntimeMap,
  schedule: string,
): ScheduleRuntime {
  return runtimes[schedule] ?? DEFAULT_RUNTIME;
}

// "Every hour" / "Every 30m" cadence label from the descriptor's interval.
export function formatCadence(intervalMinutes: number): string {
  if (intervalMinutes % 60 === 0) {
    const hours = intervalMinutes / 60;
    return hours === 1 ? "Every hour" : `Every ${hours}h`;
  }
  return `Every ${intervalMinutes}m`;
}
