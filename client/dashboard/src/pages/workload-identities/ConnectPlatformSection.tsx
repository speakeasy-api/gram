import { InlineEmptyState } from "@/components/inline-empty-state";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Label } from "@/components/ui/Label";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { WorkloadConnectionEndpoint } from "@gram/client/models/components/workloadconnectionendpoint.js";
import { useWorkloadConnectionDetails } from "@gram/client/react-query/workloadConnectionDetails.js";
import { type ReactNode, useState } from "react";
import { NO_VALUES, NOT_READY_MESSAGES } from "./connectionReadiness";
import { McpServerSelect } from "./McpServerSelect";
import { readableSelection, useReadableMcpServers } from "./readableMcpServers";

function ConnectionValue({
  label,
  value,
  note,
}: {
  label: string;
  value: string;
  note?: ReactNode;
}): JSX.Element {
  return (
    <Stack gap={1}>
      <Text muted small>
        {label}
      </Text>
      <div className="bg-muted flex items-center gap-2 px-3 py-2">
        <code className="min-w-0 flex-1 text-xs break-all">{value}</code>
        <CopyButton
          text={value}
          size="xs"
          tooltip={`Copy ${label.toLowerCase()}`}
          className="shrink-0"
        />
      </div>
      {note && (
        <Text muted small>
          {note}
        </Text>
      )}
    </Stack>
  );
}

function tokenEndpointNote(endpoint: WorkloadConnectionEndpoint): string {
  if (endpoint.onAuthenticationHost) {
    return "On Gram's authentication host, deliberately a different host from the MCP server's API host: some platforms require the token endpoint to be separate from the hosts the token is sent to.";
  }
  return "On the MCP server's own host. Platforms that require a token endpoint separate from the API host need the server's sign-in moved to Gram's authentication host.";
}

function ConnectionEndpoint({
  endpoint,
}: {
  endpoint: WorkloadConnectionEndpoint;
}): JSX.Element {
  const reason = endpoint.ready ? undefined : endpoint.notReadyReason;
  return (
    <Stack gap={4}>
      {reason && (
        <Alert variant="warning" alignTop>
          <div className="text-sm break-words">
            <p className="font-medium">Not ready: exchanges will fail</p>
            <p>{NOT_READY_MESSAGES[reason]}</p>
          </div>
        </Alert>
      )}
      {!(reason && NO_VALUES.has(reason)) && (
        <>
          <ConnectionValue
            label="Token endpoint"
            value={endpoint.tokenEndpoint}
            note={tokenEndpointNote(endpoint)}
          />
          <ConnectionValue
            label="Authorization server issuer"
            value={endpoint.issuer}
            note="The assertion's aud must be exactly this issuer or the token endpoint URL. Nothing else on that host is accepted."
          />
        </>
      )}
      <ConnectionValue
        label="Resource (MCP server URL)"
        value={endpoint.resourceUrl}
      />
      <ConnectionValue
        label="API host"
        value={endpoint.apiHost}
        note="List this among the platform's allowed API hosts."
      />
    </Stack>
  );
}

function ConnectionDetails({
  mcpServerId,
}: {
  mcpServerId: string;
}): JSX.Element {
  const { data, isPending, isError, refetch } = useWorkloadConnectionDetails(
    { mcpServerId },
    undefined,
    { throwOnError: false },
  );

  if (isPending) return <SkeletonTable />;
  if (isError) {
    return (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load the connection details"
        description="You may not have access to this MCP server, or the request failed. Try again in a moment."
        action={
          <Button size="sm" variant="secondary" onClick={() => void refetch()}>
            <Button.Text>Try again</Button.Text>
          </Button>
        }
      />
    );
  }
  if (data.endpoints.length === 0) {
    return (
      <InlineEmptyState
        icon="server"
        heading="This MCP server has no address"
        description="Give the server an address before pointing a platform at it."
      />
    );
  }
  return (
    <Stack gap={8}>
      {data.endpoints.map((endpoint) => (
        <ConnectionEndpoint key={endpoint.resourceUrl} endpoint={endpoint} />
      ))}
    </Stack>
  );
}

/**
 * What to enter in the platform's own console so its workloads can exchange
 * their identity tokens at an MCP server. Every value is read from the server,
 * which derives it the same way the MCP server's discovery document does.
 */
export function ConnectPlatformSection(): JSX.Element {
  const servers = useReadableMcpServers();
  const [selected, setSelected] = useState("");
  const { groups } = servers;
  const current = readableSelection(groups, selected);

  let body: ReactNode;
  if (servers.isPending) {
    body = <SkeletonTable />;
  } else if (servers.isError) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load MCP servers"
        description="The MCP server list failed to load. Try again in a moment."
        action={
          <Button size="sm" variant="secondary" onClick={servers.refetch}>
            <Button.Text>Try again</Button.Text>
          </Button>
        }
      />
    );
  } else if (servers.total === 0) {
    body = (
      <InlineEmptyState
        icon="server"
        heading="No MCP servers yet"
        description="Create an MCP server, then come back for the values to point this platform at."
      />
    );
  } else if (groups.length === 0) {
    body = (
      <InlineEmptyState
        icon="lock"
        heading="No MCP servers you can view"
        description="Reading a server's connection values takes read access to that MCP server. Ask an admin for access."
      />
    );
  } else {
    body = (
      <Stack gap={6}>
        <Stack gap={2} className="max-w-md">
          <Label htmlFor="connect-mcp-server">MCP server</Label>
          <McpServerSelect
            id="connect-mcp-server"
            groups={groups}
            value={current}
            onChange={setSelected}
            className="w-full"
          />
        </Stack>
        {current !== "" && <ConnectionDetails mcpServerId={current} />}
      </Stack>
    );
  }

  return (
    <section className="mt-10" aria-labelledby="connect-platform-title">
      <h2 id="connect-platform-title" className="text-display-xs font-thin">
        Connect this platform
      </h2>
      <Text muted className="mt-1 mb-4">
        Pick the MCP server this platform's workloads will call, then enter
        these values in the platform's console.
      </Text>
      {body}
    </section>
  );
}
