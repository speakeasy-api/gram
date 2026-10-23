import { InlineEmptyState } from "@/components/inline-empty-state";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { HumanizeDateTime } from "@/lib/dates";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";

function diagnosticMessage(attributes: unknown): string {
  if (attributes && typeof attributes === "object") {
    const attrs = attributes as Record<string, unknown>;
    const error = attrs.error;
    const nested =
      error && typeof error === "object"
        ? (error as Record<string, unknown>).message
        : undefined;
    const message = attrs["error.message"] ?? nested;
    if (typeof message === "string" && message) return message;
  }
  return "A server could not be listed and its tools were omitted.";
}

export function GatewayDiscoveryFailures({
  metaMcpServerId,
  from,
  to,
}: {
  metaMcpServerId: string;
  from: Date;
  to: Date;
}): JSX.Element | null {
  const client = useGramContext();
  const canRead = useCanViewToolLogs();
  const [open, setOpen] = useState(false);
  // Detailed discovery records are fetched only when opened, not on the
  // activity overview's summary-backed first paint.
  const query = useLogsEnabledErrorCheck(
    useQuery({
      queryKey: [
        "gateway-discovery-failures",
        metaMcpServerId,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: () =>
        unwrapAsync(
          telemetrySearchLogs(client, {
            searchLogsPayload: {
              from,
              to,
              limit: 20,
              sort: "desc",
              filter: {
                eventSource: "meta_discovery",
                gramUrn: `metamcp:${metaMcpServerId}:tools/list`,
              },
            },
          }),
        ),
      enabled: open && canRead,
      placeholderData: keepPreviousData,
      throwOnError: false,
    }),
  );
  if (!canRead) return null;

  let content: ReactNode;
  if (query.isLoading) {
    content = <Skeleton className="h-24 w-full" />;
  } else if (query.isLogsDisabled) {
    content = (
      <Text muted>
        Enable logs for this organization to record discovery diagnostics.
      </Text>
    );
  } else if (query.isError) {
    content = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Could not load discovery diagnostics"
        action={
          <Button size="sm" onClick={() => void query.refetch()}>
            Retry
          </Button>
        }
      />
    );
  } else if (!query.data?.logs.length) {
    content = (
      <Text muted>No omitted servers were recorded in this time range.</Text>
    );
  } else {
    content = (
      <ul className="divide-y">
        {query.data.logs.map((log) => (
          <li key={log.id} className="flex flex-col gap-1 py-3">
            <Text muted small>
              <HumanizeDateTime
                date={new Date(Number(log.timeUnixNano) / 1_000_000)}
              />
            </Text>
            <Text className="break-words">
              {diagnosticMessage(log.attributes)}
            </Text>
          </li>
        ))}
      </ul>
    );
  }
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="border">
      <CollapsibleTrigger className="group hover:bg-muted/40 flex w-full items-center justify-between gap-3 p-4 text-left">
        <Text>Discovery diagnostics</Text>
        <Icon
          name="chevron-down"
          className="size-4 shrink-0 transition-transform group-data-[state=open]:rotate-180"
        />
      </CollapsibleTrigger>
      <CollapsibleContent
        className="space-y-3 border-t p-4"
        aria-busy={query.isFetching}
      >
        <Text muted small>
          Latest 20 omissions in this range. These records do not show current
          server health. Reconnect your client after recovery to refresh its
          tools.
        </Text>
        {content}
      </CollapsibleContent>
    </Collapsible>
  );
}
