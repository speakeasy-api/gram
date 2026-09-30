import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import type { WorkloadConnectionEndpoint } from "@gram/client/models/components/workloadconnectionendpoint.js";
import { useWorkloadConnectionDetails } from "@gram/client/react-query/workloadConnectionDetails.js";
import { TriangleAlert } from "lucide-react";
import { type ReactNode, useId, useState } from "react";
import {
  NO_VALUES,
  NOT_READY_MESSAGES,
  NOT_READY_SHORT,
} from "./connectionReadiness";
import { McpServerSelect } from "./McpServerSelect";
import { readableSelection, useReadableMcpServers } from "./readableMcpServers";

function Note({
  children,
  tooltip,
  warning,
}: {
  children: ReactNode;
  tooltip?: string;
  warning?: boolean;
}): JSX.Element {
  const note = (
    <span
      tabIndex={tooltip ? 0 : undefined}
      className="inline-flex items-center gap-1"
    >
      {warning && (
        <TriangleAlert
          aria-hidden
          className="text-default-warning size-3.5 shrink-0"
        />
      )}
      <Text as="span" small muted={!warning} warning={warning}>
        {children}
      </Text>
    </span>
  );
  if (!tooltip) return note;
  return <SimpleTooltip tooltip={tooltip}>{note}</SimpleTooltip>;
}

function Retry({ onClick }: { onClick: () => void }): JSX.Element {
  return (
    <Button type="button" size="xs" variant="tertiary" onClick={onClick}>
      <Button.Text>Try again</Button.Text>
    </Button>
  );
}

function EndpointRow({
  endpoint,
}: {
  endpoint: WorkloadConnectionEndpoint;
}): JSX.Element {
  const reason = endpoint.ready ? undefined : endpoint.notReadyReason;
  if (reason && NO_VALUES.has(reason)) {
    return (
      <Note warning tooltip={NOT_READY_MESSAGES[reason]}>
        {NOT_READY_SHORT[reason]}
      </Note>
    );
  }
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
      <span className="flex min-w-0 items-center gap-1">
        <code className="bg-muted min-w-0 px-2 py-1 text-xs break-all">
          {endpoint.tokenEndpoint}
        </code>
        <CopyButton
          text={endpoint.tokenEndpoint}
          size="xs"
          tooltip="Copy token endpoint"
          className="shrink-0"
        />
      </span>
      {reason && (
        <Note warning tooltip={NOT_READY_MESSAGES[reason]}>
          {NOT_READY_SHORT[reason]}
        </Note>
      )}
    </span>
  );
}

// One row per distinct token endpoint: a server answering on several addresses
// usually shares one.
function distinctEndpoints(
  endpoints: WorkloadConnectionEndpoint[],
): WorkloadConnectionEndpoint[] {
  const seen = new Set<string>();
  return endpoints.filter((endpoint) => {
    const key = `${endpoint.tokenEndpoint}|${endpoint.notReadyReason ?? ""}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

function TokenEndpointValue({
  mcpServerId,
}: {
  mcpServerId: string;
}): JSX.Element {
  const { data, isPending, isError, refetch } = useWorkloadConnectionDetails(
    { mcpServerId },
    undefined,
    { throwOnError: false },
  );

  if (isPending) return <Skeleton className="h-6 w-64" />;
  if (isError) {
    return (
      <span className="inline-flex items-center gap-1">
        <Note warning>Couldn&apos;t load the token endpoint</Note>
        <Retry onClick={() => void refetch()} />
      </span>
    );
  }
  if (data.endpoints.length === 0) {
    return <Note>This MCP server has no address</Note>;
  }
  return (
    <span className="flex min-w-0 flex-col gap-1">
      {distinctEndpoints(data.endpoints).map((endpoint) => (
        <EndpointRow key={endpoint.resourceUrl} endpoint={endpoint} />
      ))}
    </span>
  );
}

/**
 * The token endpoint a platform must exchange its workloads' identity tokens
 * at, for an MCP server the viewer picks. The endpoint is read from the server,
 * never assembled here, so it follows each environment's hosts.
 */
export function TokenEndpointPicker({
  className,
}: {
  className?: string;
}): JSX.Element {
  const labelId = useId();
  const servers = useReadableMcpServers();
  const [selected, setSelected] = useState("");
  const current = readableSelection(servers.groups, selected);

  let body: ReactNode;
  if (servers.isPending) {
    body = <Skeleton className="h-8 w-48" />;
  } else if (servers.isError) {
    body = (
      <span className="inline-flex items-center gap-1">
        <Note warning>Couldn&apos;t load MCP servers</Note>
        <Retry onClick={servers.refetch} />
      </span>
    );
  } else if (servers.total === 0) {
    body = <Note>Create an MCP server to get one</Note>;
  } else if (servers.groups.length === 0) {
    body = (
      <Note tooltip="Reading a server's token endpoint takes read access to that MCP server. Ask an admin for access.">
        No MCP servers you can view
      </Note>
    );
  } else {
    body = (
      <>
        <McpServerSelect
          id={`${labelId}-server`}
          groups={servers.groups}
          value={current}
          onChange={setSelected}
          size="sm"
          className="w-48 max-w-full"
          ariaLabel="MCP server"
        />
        {current !== "" && <TokenEndpointValue mcpServerId={current} />}
      </>
    );
  }

  return (
    <div
      role="group"
      aria-labelledby={labelId}
      className={cn(
        "flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2",
        className,
      )}
    >
      <span id={labelId} className="text-eyebrow">
        Token endpoint
      </span>
      {body}
    </div>
  );
}
