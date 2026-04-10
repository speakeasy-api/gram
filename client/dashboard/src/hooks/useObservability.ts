import { useSearchLogsMutation } from "@gram/client/react-query";
import { useGetObservabilityOverview } from "@gram/client/react-query";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult";
import { useCallback, useEffect, useMemo } from "react";

export type SearchLogsData = {
  logs: TelemetryLogRecord[];
  nextCursor?: string;
  isLoading: boolean;
  error: Error | null;
  search: (filter?: { from?: Date; to?: Date }) => void;
};

export type ObservabilityOverviewData = {
  overview: GetObservabilityOverviewResult | undefined;
  isLoading: boolean;
  error: Error | null;
};

/**
 * Hook to fetch search logs from the telemetry API.
 * Wraps the SDK's searchLogs mutation and triggers an initial fetch.
 */
export function useSearchLogs(): SearchLogsData {
  const mutation = useSearchLogsMutation();

  const search = useCallback(
    (filter?: { from?: Date; to?: Date }) => {
      mutation.mutate({
        request: {
          searchLogsPayload: {
            from: filter?.from,
            to: filter?.to,
            limit: 50,
          },
        },
      });
    },
    [mutation.mutate],
  );

  // Trigger initial fetch on mount
  useEffect(() => {
    search();
  }, [search]);

  return useMemo(
    () => ({
      logs: mutation.data?.logs ?? [],
      nextCursor: mutation.data?.nextCursor,
      isLoading: mutation.isPending,
      error: mutation.error ? (mutation.error as Error) : null,
      search,
    }),
    [mutation.data, mutation.isPending, mutation.error, search],
  );
}

/**
 * Hook to fetch observability overview stats from the telemetry API.
 * Wraps the SDK's getObservabilityOverview query.
 */
export function useObservabilityOverview(options: {
  from: Date;
  to: Date;
}): ObservabilityOverviewData {
  const query = useGetObservabilityOverview({
    getObservabilityOverviewPayload: {
      from: options.from,
      to: options.to,
    },
  });

  return useMemo(
    () => ({
      overview: query.data,
      isLoading: query.isLoading,
      error: query.error ? (query.error as Error) : null,
    }),
    [query.data, query.isLoading, query.error],
  );
}
