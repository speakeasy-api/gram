import { InlineEmptyState } from "@/components/inline-empty-state";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { hasScopeInGrants, useRBAC } from "@/hooks/useRBAC";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type {
  NotReadyReason,
  WorkloadConnectionEndpoint,
} from "@gram/client/models/components/workloadconnectionendpoint.js";
import { useListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { useWorkloadConnectionDetails } from "@gram/client/react-query/workloadConnectionDetails.js";
import { type ReactNode, useMemo, useState } from "react";

const NOT_READY_MESSAGES: Record<NotReadyReason, string> = {
  not_publicly_reachable:
    "This MCP server can't be reached publicly: it is disabled, or reachable only through a private network. A platform's token exchange will fail.",
  no_authorization_server:
    "Gram isn't this MCP server's authorization server, because the server isn't protected by Gram sign-in. There is no token endpoint to exchange at until it is.",
  workload_grant_unavailable:
    "This MCP server's authorization server doesn't advertise the jwt-bearer grant, so it accepts no workload identity tokens. Every exchange will fail, whatever the platform is configured with.",
  agent_rollout_disabled:
    "Agent authorization isn't enabled for this organization, and the token endpoint refuses workload identity tokens without it. The values below are correct, but every exchange will fail until it is enabled.",
};

// The reasons after which there is no token endpoint worth copying.
const NO_VALUES: ReadonlySet<NotReadyReason> = new Set<NotReadyReason>([
  "not_publicly_reachable",
  "no_authorization_server",
]);

type ServerGroup = {
  projectId: string;
  projectName: string;
  servers: McpServer[];
};

// The connection details read requires mcp:read on the server, written against
// its toolset when it is toolset-backed, so only those servers are offered.
function canReadServer(
  grants: Parameters<typeof hasScopeInGrants>[0],
  server: McpServer,
): boolean {
  return hasScopeInGrants(
    grants,
    "mcp:read",
    server.toolsetId ?? server.id,
    server.projectId,
  );
}

function serverLabel(server: McpServer): string {
  return server.name || server.slug || server.id;
}

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
  const organization = useOrganization();
  const { grants, isLoading: grantsLoading } = useRBAC();
  const servers = useListMcpServersForOrg(undefined, undefined, {
    throwOnError: false,
  });
  const [selected, setSelected] = useState("");

  const allServers = servers.data?.mcpServers ?? [];
  const groups = useMemo((): ServerGroup[] => {
    const projectNames = new Map(
      organization.projects.map((project) => [project.id, project.name]),
    );
    const byProject = new Map<string, ServerGroup>();
    for (const server of servers.data?.mcpServers ?? []) {
      if (!canReadServer(grants, server)) continue;
      let group = byProject.get(server.projectId);
      if (!group) {
        group = {
          projectId: server.projectId,
          projectName: projectNames.get(server.projectId) ?? "Unknown project",
          servers: [],
        };
        byProject.set(server.projectId, group);
      }
      group.servers.push(server);
    }
    return [...byProject.values()];
  }, [grants, organization.projects, servers.data]);

  // A selection only counts while the server is still in the readable list, so
  // losing access to it hides its values rather than leaving them on screen.
  const current = groups.some((group) =>
    group.servers.some((server) => server.id === selected),
  )
    ? selected
    : "";

  let body: ReactNode;
  if (servers.isPending || grantsLoading) {
    body = <SkeletonTable />;
  } else if (servers.isError) {
    body = (
      <InlineEmptyState
        icon="triangle-alert"
        heading="Couldn't load MCP servers"
        description="The MCP server list failed to load. Try again in a moment."
        action={
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void servers.refetch()}
          >
            <Button.Text>Try again</Button.Text>
          </Button>
        }
      />
    );
  } else if (allServers.length === 0) {
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
          <Select value={current} onValueChange={setSelected}>
            <SelectTrigger id="connect-mcp-server" className="w-full">
              <SelectValue placeholder="Select an MCP server" />
            </SelectTrigger>
            <SelectContent>
              {groups.map((group) => (
                <SelectGroup key={group.projectId}>
                  <SelectLabel>{group.projectName}</SelectLabel>
                  {group.servers.map((server) => (
                    <SelectItem key={server.id} value={server.id}>
                      {serverLabel(server)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              ))}
            </SelectContent>
          </Select>
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
