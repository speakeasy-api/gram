import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import type { LogFilter } from "@gram/client/models/components/logfilter";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord";
import type { ToolCallSummary } from "@gram/client/models/components/toolcallsummary";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Operator as Op } from "@gram/client/models/components/logfilter";
import type { ActiveLogFilter } from "./log-filter-types";

const PER_PAGE = 100; // fetch more logs to improve grouping coverage

function getNestedAttr(attrs: Record<string, unknown>, path: string): unknown {
  if (!attrs || typeof attrs !== "object") return undefined;
  const parts = path.split(".");
  let cur: unknown = attrs;
  for (const part of parts) {
    if (cur == null || typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[part];
  }
  return cur;
}

/**
 * Groups raw TelemetryLogRecord[] by traceId into ToolCallSummary-shaped objects.
 */
export function logsToTraceSummaries(
  logs: TelemetryLogRecord[],
): ToolCallSummary[] {
  const groups = new Map<
    string,
    { logs: TelemetryLogRecord[]; minTime: string }
  >();

  for (const log of logs) {
    const traceId = log.traceId;
    if (!traceId) continue;

    const existing = groups.get(traceId);
    if (existing) {
      existing.logs.push(log);
      if (BigInt(log.timeUnixNano) < BigInt(existing.minTime)) {
        existing.minTime = log.timeUnixNano;
      }
    } else {
      groups.set(traceId, { logs: [log], minTime: log.timeUnixNano });
    }
  }

  const summaries: ToolCallSummary[] = [];
  for (const [traceId, group] of groups) {
    // Derive gramUrn from first log's attributes
    let gramUrn = "";
    let httpStatusCode: number | undefined;

    for (const log of group.logs) {
      if (!gramUrn) {
        const urn = getNestedAttr(log.attributes, "gram.tool.urn");
        if (typeof urn === "string") gramUrn = urn;
      }
      if (httpStatusCode === undefined) {
        const code = getNestedAttr(log.attributes, "http.response.status_code");
        if (typeof code === "number") httpStatusCode = code;
      }
      if (gramUrn && httpStatusCode !== undefined) break;
    }

    summaries.push({
      traceId,
      gramUrn: gramUrn || "unknown",
      startTimeUnixNano: group.minTime,
      logCount: group.logs.length,
      ...(httpStatusCode !== undefined ? { httpStatusCode } : {}),
    });
  }

  // Sort by start time descending (most recent first).
  // Lexicographic comparison is safe here since all nanosecond timestamps are
  // the same digit-width (19 digits for current-era Unix nanoseconds).
  summaries.sort((a, b) =>
    a.startTimeUnixNano < b.startTimeUnixNano ? 1 : -1,
  );
  return summaries;
}

function toSdkFilters(filters: ActiveLogFilter[]): LogFilter[] {
  return filters.map((f) => {
    let values: string[] | undefined;
    if (f.op === Op.In) {
      values = f.value
        ?.split(",")
        .map((v) => v.trim())
        .filter(Boolean);
    } else if (f.value !== undefined) {
      values = [f.value];
    }
    return {
      path: f.path,
      operator: f.op,
      ...(values !== undefined ? { values } : {}),
    };
  });
}

/**
 * Hook that fetches logs via searchLogs with attribute filters and
 * returns data shaped like the searchToolCalls query for transparent swapping.
 */
export function useAttributeLogsQuery({
  logFilters,
  extraFilters = [],
  gramUrn,
  from,
  to,
  enabled,
}: {
  logFilters: ActiveLogFilter[];
  extraFilters?: LogFilter[];
  gramUrn: string | null;
  from: Date;
  to: Date;
  enabled: boolean;
}) {
  const client = useGramContext();

  return useInfiniteQuery({
    queryKey: [
      "attribute-logs",
      logFilters.map((f) => `${f.path}:${f.op}:${f.value ?? ""}`),
      extraFilters.map(
        (f) => `${f.path}:${f.operator}:${f.values?.join(",") ?? ""}`,
      ),
      gramUrn,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: async ({ pageParam }) => {
      const result = await unwrapAsync(
        telemetrySearchLogs(client, {
          searchLogsPayload: {
            from,
            to,
            filters: [
              {
                path: "gram.event.source",
                operator: "in",
                values: ["tool_call", "function"],
              },
              ...toSdkFilters(logFilters),
              ...extraFilters,
            ],
            ...(gramUrn ? { filter: { gramUrn } } : {}),
            cursor: pageParam,
            limit: PER_PAGE,
            sort: "desc",
          },
        }),
      );

      return {
        toolCalls: logsToTraceSummaries(result.logs),
        nextCursor: result.nextCursor,
      };
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    enabled,
    throwOnError: false,
  });
}
