import { Heading } from "@/components/ui/Heading";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { buildGetUnproxiedMcpServerUsageQuery } from "@gram/client/react-query/getUnproxiedMcpServerUsage.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { PluginStatusBanner } from "@/pages/mcp/overview/PluginStatusBanner";
import { UnproxiedMcpUsageBreakdown } from "./UnproxiedMcpUsageBreakdown";

const USAGE_WINDOW_DAYS = 30;
const MS_PER_DAY = 24 * 60 * 60 * 1000;

type UnproxiedMcpOverviewTabProps = {
  unproxiedMcpServerId: string;
  mcpServerId: string;
  mcpServerSlug: string;
  mcpServerName: string;
};

/**
 * A scoped-down stand-in for MCPOverviewTab: unproxied servers have no
 * Gram-proxied traffic, so the summary cards, top-tools breakdowns, and
 * comparison stats that tab shows (all sourced from Gram's own proxy
 * telemetry) don't apply. This shows only a daily call-count chart, sourced
 * from Shadow MCP's hook-reported traces matched by URL. Plugin/publish
 * status is backend-agnostic, so it's shown the same as every other server.
 */
export function UnproxiedMcpOverviewTab({
  unproxiedMcpServerId,
  mcpServerId,
  mcpServerSlug,
  mcpServerName,
}: UnproxiedMcpOverviewTabProps): JSX.Element {
  const { from, to } = useMemo(() => {
    const to = new Date();
    // USAGE_WINDOW_DAYS calendar buckets inclusive of `to`, so the query
    // range starts (USAGE_WINDOW_DAYS - 1) days back, matching the buckets
    // built below rather than requesting one extra day nothing displays.
    const from = new Date(to.getTime() - (USAGE_WINDOW_DAYS - 1) * MS_PER_DAY);
    return { from, to };
  }, []);

  const {
    data: server,
    isLoading: isLoadingServer,
    isError: isServerError,
  } = useGetUnproxiedMcpServer({
    id: unproxiedMcpServerId,
  });

  const client = useGramContext();
  // The generated hook's query key only covers auth params, not the request
  // body, so navigating between two servers' Overview tabs without a full
  // remount would otherwise keep showing the first server's cached chart.
  // Build the query manually to override it -- the convenience hook's typed
  // options deliberately exclude queryKey.
  const {
    data,
    isLoading,
    isError: isUsageError,
  } = useQuery({
    ...buildGetUnproxiedMcpServerUsageQuery(client, {
      getUnproxiedMcpServerUsageRequestBody: {
        url: server?.url ?? "",
        from,
        to,
      },
    }),
    queryKey: [
      "unproxied-mcp-usage-chart",
      server?.url,
      from.toISOString(),
      to.toISOString(),
    ],
    enabled: !!server?.url,
    throwOnError: false,
  });

  // Zero-fill days the backend omitted so a sparse activity pattern renders
  // with real gaps instead of bars that look visually adjacent in time.
  // Counts back USAGE_WINDOW_DAYS buckets from `to` (inclusive) rather than
  // walking date <= to from `from` (inclusive), which double-counts the
  // boundary and produces one extra bucket.
  const buckets = useMemo(() => {
    const byDate = new Map(
      (data?.buckets ?? []).map((bucket) => [bucket.date, bucket.callCount]),
    );
    const days: { date: string; callCount: number }[] = [];
    for (let i = USAGE_WINDOW_DAYS - 1; i >= 0; i--) {
      const date = new Date(to.getTime() - i * MS_PER_DAY)
        .toISOString()
        .slice(0, 10);
      days.push({ date, callCount: byDate.get(date) ?? 0 });
    }
    return days;
  }, [data, to]);
  const hasActivity = buckets.some((bucket) => bucket.callCount > 0);
  const maxCount = Math.max(1, ...buckets.map((bucket) => bucket.callCount));
  const isUsageUnavailable = isUsageError || isServerError;
  const isLoadingUsage = !isUsageUnavailable && (isLoading || isLoadingServer);

  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      <Stack gap={6}>
        <PluginStatusBanner
          server={{
            kind: "mcp-server",
            id: mcpServerId,
            slug: mcpServerSlug,
            name: mcpServerName,
          }}
        />

        <div className="border-neutral-softest border p-6">
          <Heading variant="h4">Tool calls over time</Heading>
          <Text small muted className="mt-1">
            Sourced from Shadow MCP activity in the last {USAGE_WINDOW_DAYS}{" "}
            days. This requires the Gram hook integration to be installed, and
            only reflects calls made from hook-instrumented sessions. A freshly
            added or rarely used server may show no data even when it's working
            correctly.
          </Text>

          <div className="mt-6">
            {isLoadingUsage ? (
              <Skeleton className="h-32 w-full" />
            ) : isUsageUnavailable ? (
              <Text small muted>
                Couldn't load activity for this server. Try refreshing the page.
              </Text>
            ) : !hasActivity ? (
              <Text small muted>
                No activity observed yet.
              </Text>
            ) : (
              <div className="flex items-end justify-start gap-3 overflow-x-auto pb-1">
                {buckets.map((bucket) => (
                  <div
                    key={bucket.date}
                    className="flex w-12 flex-col items-center gap-1"
                    title={`${bucket.date}: ${bucket.callCount} call${bucket.callCount === 1 ? "" : "s"}`}
                  >
                    <div className="flex h-24 w-full items-end">
                      <div
                        className="bg-primary/70 w-full"
                        style={{
                          height:
                            bucket.callCount === 0
                              ? "0%"
                              : `${Math.max(4, (bucket.callCount / maxCount) * 100)}%`,
                        }}
                      />
                    </div>
                    <Text small muted className="text-center">
                      {bucket.callCount}
                    </Text>
                    <Text small muted className="text-center text-xs">
                      {bucket.date.slice(5)}
                    </Text>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {server?.url && (
          <div className="border-neutral-softest border p-6">
            <Heading variant="h4">Usage breakdown</Heading>
            <Text small muted className="mt-1">
              Same Shadow MCP-sourced activity as the chart above, broken down
              by tool, user, and client.
            </Text>
            <div className="mt-6">
              <UnproxiedMcpUsageBreakdown
                url={server.url}
                from={from}
                to={to}
              />
            </div>
          </div>
        )}
      </Stack>
    </div>
  );
}
