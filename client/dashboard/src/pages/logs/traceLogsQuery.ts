import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import type { ToolUsageTraceLogGroup } from "@gram/client/models/components/toolusagetraceloggroup.js";
import { Operator as Op } from "@gram/client/models/components/logfilter";
import type { SearchLogsPayload } from "@gram/client/models/components/searchlogspayload";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import type { UseQueryOptions } from "@tanstack/react-query";
import type { SearchLogsResult } from "@gram/client/models/components/searchlogsresult";

function buildTraceFilters(
  logGroup: ToolUsageTraceLogGroup,
): Pick<SearchLogsPayload, "filter" | "filters"> | null {
  if (logGroup.kind === "correlation_id") {
    return {
      filters: [
        {
          path: "gram.trigger.correlation_id",
          operator: Op.Eq,
          values: [logGroup.value],
        },
      ],
    };
  }
  if (logGroup.kind === "trigger_event_id") {
    return {
      filters: [
        {
          path: "gram.trigger.event_id",
          operator: Op.Eq,
          values: [logGroup.value],
        },
      ],
    };
  }
  if (logGroup.kind === "trace_id") {
    return { filter: { traceId: logGroup.value } };
  }
  return null;
}

export function traceLogsQueryOptions(
  client: ReturnType<typeof useGramContext>,
  logGroup: ToolUsageTraceLogGroup,
  from: Date,
  to: Date,
): UseQueryOptions<SearchLogsResult> & { enabled: boolean } {
  const traceFilters = buildTraceFilters(logGroup);
  return {
    queryKey: [
      "trace-logs",
      logGroup.kind,
      logGroup.value,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () =>
      unwrapAsync(
        telemetrySearchLogs(client, {
          searchLogsPayload: {
            ...traceFilters,
            from,
            to,
            limit: 100,
            sort: "asc",
          },
        }),
      ),
    enabled: traceFilters !== null,
  };
}
