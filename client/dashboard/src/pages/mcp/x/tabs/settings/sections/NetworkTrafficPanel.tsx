import { useMemo, useState } from "react";

import { NetworkTrafficChart } from "./NetworkTrafficChart";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Text } from "@/components/ui/Text";
import type { Window } from "@gram/client/models/components/getmcpnetworktrafficpayload.js";
import { useGetMcpNetworkTraffic } from "@gram/client/react-query/getMcpNetworkTraffic.js";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";

const WINDOW_OPTIONS: { value: Window; label: string }[] = [
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
];

export function NetworkTrafficPanel({
  mcpServerId,
  metaMcpServerId,
}: {
  mcpServerId?: string;
  metaMcpServerId?: string;
}): JSX.Element {
  const [window, setWindow] = useState<Window>("7d");
  const traffic = useLogsEnabledErrorCheck(
    useGetMcpNetworkTraffic(
      { getMcpNetworkTrafficPayload: { mcpServerId, metaMcpServerId, window } },
      undefined,
      { retry: false, throwOnError: false },
    ),
  );

  const points = useMemo(
    () =>
      (traffic.data?.points ?? []).map((point) => ({
        bucketStart: point.bucketStart.toISOString(),
        publicRequests: point.publicRequests,
        privateRequests: point.privateRequests,
      })),
    [traffic.data],
  );

  return (
    <div className="space-y-3">
      <div className="flex items-start justify-between gap-3">
        <div className="space-y-1">
          <Text className="block text-sm font-medium">
            Public and private traffic
          </Text>
          <Text small muted className="block">
            Check which route clients still use before switching to public only
            or private only.
          </Text>
        </div>
        <SegmentedControl
          value={window}
          onChange={setWindow}
          options={WINDOW_OPTIONS}
          disabled={traffic.isLogsDisabled}
        />
      </div>
      {traffic.isLogsDisabled ? (
        <div className="border-border text-muted-foreground border p-4 text-sm">
          Enable observability for this organization to see which network route
          clients use.
        </div>
      ) : (
        <NetworkTrafficChart
          points={points}
          loading={traffic.isPending}
          error={traffic.isError}
          windowStart={traffic.data?.from.toISOString()}
          windowEnd={traffic.data?.to.toISOString()}
          lastPublicAt={traffic.data?.lastPublicAt}
          lastPrivateAt={traffic.data?.lastPrivateAt}
        />
      )}
    </div>
  );
}
