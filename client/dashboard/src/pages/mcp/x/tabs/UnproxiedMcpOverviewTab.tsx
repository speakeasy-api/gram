import { Heading } from "@/components/ui/Heading";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { useGetUnproxiedMcpServerUsage } from "@gram/client/react-query/getUnproxiedMcpServerUsage.js";
import { useMemo } from "react";
import { PluginStatusBanner } from "@/pages/mcp/overview/PluginStatusBanner";

const USAGE_WINDOW_DAYS = 30;

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
    const from = new Date(
      to.getTime() - USAGE_WINDOW_DAYS * 24 * 60 * 60 * 1000,
    );
    return { from, to };
  }, []);

  const { data: server } = useGetUnproxiedMcpServer({
    id: unproxiedMcpServerId,
  });

  const {
    data,
    isLoading,
    isError: isUsageError,
  } = useGetUnproxiedMcpServerUsage(
    {
      getUnproxiedMcpServerUsageRequestBody: {
        url: server?.url ?? "",
        from,
        to,
      },
    },
    undefined,
    { enabled: !!server?.url, throwOnError: false },
  );

  // Zero-fill days the backend omitted so a sparse activity pattern renders
  // with real gaps instead of bars that look visually adjacent in time.
  const buckets = useMemo(() => {
    const byDate = new Map(
      (data?.buckets ?? []).map((bucket) => [bucket.date, bucket.callCount]),
    );
    const days: { date: string; callCount: number }[] = [];
    for (let day = new Date(from); day <= to; day.setDate(day.getDate() + 1)) {
      const date = day.toISOString().slice(0, 10);
      days.push({ date, callCount: byDate.get(date) ?? 0 });
    }
    return days;
  }, [data, from, to]);
  const hasActivity = buckets.some((bucket) => bucket.callCount > 0);
  const maxCount = Math.max(1, ...buckets.map((bucket) => bucket.callCount));
  const isLoadingUsage = isLoading || !server;

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

        <div className="border-neutral-softest rounded-lg border p-6">
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
            ) : isUsageError ? (
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
                        className="bg-primary/70 w-full rounded-t"
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
      </Stack>
    </div>
  );
}
