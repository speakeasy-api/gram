import { ChartCard } from "@/components/chart/ChartCard";
import { ACCENT_RED } from "@/components/chart/palette";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { Text } from "@/components/ui/Text";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import {
  discoveryChartData,
  diagnosticMessage,
  tooltipLines,
  type DiscoveryFailures,
} from "./gatewayDiscoveryChart";

export function GatewayDiscoveryFailures({
  metaMcpServerId,
  from,
  to,
  failures,
  loading,
  error,
  expandedChart,
  onExpand,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  metaMcpServerId: string;
  from: Date;
  to: Date;
  failures: DiscoveryFailures | undefined;
  loading: boolean;
  error: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onRangeSelect: (from: Date, to: Date) => void;
  isZoomed: boolean;
  onResetZoom: () => void;
}): JSX.Element | null {
  const client = useGramContext();
  const canRead = useCanViewToolLogs();
  const [hovered, setHovered] = useState<{
    index: number;
    range: string;
  } | null>(null);
  const range = `${metaMcpServerId}/${from.toISOString()}/${to.toISOString()}`;
  const data = useMemo(
    () => discoveryChartData(failures, from, to),
    [failures, from, to],
  );
  const index = hovered?.range === range ? hovered.index : null;
  const timestamp = index === null ? undefined : data.timestamps[index];
  const count = index === null ? 0 : (data.counts[index] ?? 0);
  // Buckets align to UTC, so the first can start before the selected range.
  const bucketFrom = Math.max(timestamp ?? from.getTime(), from.getTime());
  // searchLogs uses an inclusive end and Date has millisecond precision.
  // Stop before the next bucket; sub-millisecond tail events are not sampled.
  const bucketTo = Math.min(
    (timestamp ?? 0) + data.bucketMs - 1,
    to.getTime() - 1,
  );
  const details = useQuery({
    queryKey: [
      "gateway-discovery-events",
      metaMcpServerId,
      bucketFrom,
      bucketTo,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetrySearchLogs(client, {
          searchLogsPayload: {
            from: new Date(bucketFrom),
            to: new Date(bucketTo),
            limit: 1,
            sort: "desc",
            filter: {
              eventSource: "meta_discovery",
              gramUrn: `metamcp:${metaMcpServerId}:tools/list`,
            },
          },
        }),
      ),
    enabled:
      canRead && failures !== undefined && count > 0 && !loading && !error,
    staleTime: 60_000,
    throwOnError: false,
  });
  if (!canRead) return null;
  const hasData = data.counts.some((value) => value > 0);
  const datasets = [
    {
      label: "Omitted members",
      data: data.counts,
      backgroundColor: ACCENT_RED,
      hoverBackgroundColor: ACCENT_RED,
    },
  ];
  const eventLines = (dataIndex: number): string[] => {
    if (dataIndex !== index || count === 0) return [];
    if (details.isError)
      return ["Could not load event details.", "Hover again to retry."];
    if (!details.data) return ["Loading event details…"];
    const log = details.data.logs[0];
    if (!log) return ["Event details are no longer available."];
    return [
      "Latest event",
      new Date(Number(log.timeUnixNano) / 1_000_000).toLocaleTimeString([], {
        hour: "numeric",
        minute: "2-digit",
        second: "2-digit",
      }),
      ...tooltipLines(diagnosticMessage(log.attributes)),
    ];
  };
  // A server that predates discovery failures answers without the series,
  // which is unknown rather than a range with no omissions.
  let content = (
    <WidgetEmptyState
      message={
        failures === undefined
          ? "Discovery failures are not available from this server yet"
          : "No omitted servers were recorded in this time range"
      }
      className="h-[260px]"
    />
  );
  if (hasData) {
    content = (
      <>
        <StackedTimeBarChart
          {...data}
          datasets={datasets}
          height={expandedChart === "gateway-discovery-failures" ? 420 : 260}
          expanded={expandedChart === "gateway-discovery-failures"}
          onRangeSelect={onRangeSelect}
          onBucketHover={(index) =>
            setHovered((previous) => {
              if (index === null) return null;
              if (previous?.index === index && previous.range === range)
                return previous;
              return { index, range };
            })
          }
          tooltipAfterBody={eventLines}
        />
        <Text muted small>
          Counts omitted members from Direct tool listings, in UTC intervals of
          at least 30 minutes. Hover or tap an interval to see its latest
          omission. Reconnect your client after recovery to refresh its tools.
        </Text>
      </>
    );
  }
  return (
    <ChartCard
      title="Discovery failures over time"
      chartId="gateway-discovery-failures"
      expandedChart={expandedChart}
      onExpand={onExpand}
      loading={loading}
      error={error}
      hasData={hasData}
      isZoomed={isZoomed}
      onResetZoom={onResetZoom}
    >
      {content}
    </ChartCard>
  );
}
