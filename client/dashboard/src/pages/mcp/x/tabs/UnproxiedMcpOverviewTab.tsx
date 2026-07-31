import { Heading } from "@/components/ui/Heading";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { useGetUnproxiedMcpServerUsage } from "@gram/client/react-query/getUnproxiedMcpServerUsage.js";
import { useMemo } from "react";

const USAGE_WINDOW_DAYS = 30;

type UnproxiedMcpOverviewTabProps = {
  unproxiedMcpServerId: string;
};

/**
 * A scoped-down stand-in for MCPOverviewTab: unproxied servers have no
 * Gram-proxied traffic, so the summary cards, top-tools breakdowns, and
 * comparison stats that tab shows (all sourced from Gram's own proxy
 * telemetry) don't apply. This shows only a daily call-count chart, sourced
 * from Shadow MCP's hook-reported traces matched by URL.
 */
export function UnproxiedMcpOverviewTab({
  unproxiedMcpServerId,
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

  const { data, isLoading } = useGetUnproxiedMcpServerUsage(
    {
      getUnproxiedMcpServerUsageRequestBody: {
        url: server?.url ?? "",
        from,
        to,
      },
    },
    undefined,
    { enabled: !!server?.url },
  );

  const buckets = data?.buckets ?? [];
  const maxCount = Math.max(1, ...buckets.map((bucket) => bucket.callCount));
  const isLoadingUsage = isLoading || !server;

  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      <div className="border-neutral-softest rounded-lg border p-6">
        <Heading variant="h4">Tool calls over time</Heading>
        <Text small muted className="mt-1">
          Sourced from Shadow MCP activity in the last {USAGE_WINDOW_DAYS} days
          — this requires the Gram hook integration to be installed, and only
          reflects calls made from hook-instrumented sessions. A freshly added
          or rarely used server may show no data even when it's working
          correctly.
        </Text>

        <div className="mt-6">
          {isLoadingUsage ? (
            <Skeleton className="h-32 w-full" />
          ) : buckets.length === 0 ? (
            <Text small muted>
              No activity observed yet.
            </Text>
          ) : (
            <div className="flex h-32 items-end gap-1">
              {buckets.map((bucket) => (
                <div
                  key={bucket.date}
                  className="bg-primary/70 min-w-[4px] flex-1 rounded-t"
                  style={{
                    height: `${Math.max(4, (bucket.callCount / maxCount) * 100)}%`,
                  }}
                  title={`${bucket.date}: ${bucket.callCount} call${bucket.callCount === 1 ? "" : "s"}`}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
