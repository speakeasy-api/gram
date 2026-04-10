import type { SearchLogsResult } from "@gram/client/models/components/searchlogsresult";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult";

export type SearchLogsData = {
  logs: SearchLogsResult["logs"];
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
 */
export function useSearchLogs(): SearchLogsData {
  // TODO: implement with useSearchLogsMutation
  throw new Error("useSearchLogs not implemented");
}

/**
 * Hook to fetch observability overview stats from the telemetry API.
 */
export function useObservabilityOverview(_options: {
  from: Date;
  to: Date;
}): ObservabilityOverviewData {
  // TODO: implement with useGetObservabilityOverview
  throw new Error("useObservabilityOverview not implemented");
}
