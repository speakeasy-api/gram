import { DetailHero } from "@/components/detail-hero";
import { MCPServerCard } from "@/components/mcp/MCPServerCard";
import { Page } from "@/components/page-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { RequireScope } from "@/components/require-scope";
import {
  SourceInfoRow,
  SourceInfoTable,
} from "@/components/sources/SourceInfoTable";
import { CopyButton } from "@/components/ui/CopyButton";
import { Heading } from "@/components/ui/Heading";
import { Input } from "@/components/ui/Input";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useTelemetry } from "@/contexts/Telemetry";
import { dateTimeFormatters } from "@/lib/dates";
import {
  formatTunneledMcpDisplay,
  getTunneledMcpServerArgs,
} from "@/lib/sources";
import { TUNNELED_MCP_FEATURE_FLAG } from "@/lib/tunneledMcp";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpConnection } from "@gram/client/models/components/tunneledmcpconnection.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import type { TunneledMcpServerConnections } from "@gram/client/models/components/tunneledmcpserverconnections.js";
import {
  invalidateAllGetTunneledMcpServer,
  useGetTunneledMcpServer,
} from "@gram/client/react-query/getTunneledMcpServer.js";
import { useListTunneledMcpServerConnections } from "@gram/client/react-query/listTunneledMcpServerConnections.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import { useUpdateTunneledMcpServerMutation } from "@gram/client/react-query/updateTunneledMcpServer.js";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Field, FieldError } from "@/components/ui/Field";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import {
  KeyRound,
  Loader2,
  Network,
  Plus,
  RotateCcw,
  Server,
  Trash2,
} from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Navigate, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import {
  useLinkMcpServerToTunneled,
  useRotateTunneledMcpServerKey,
  type RotateTunneledMcpServerKeyData,
} from "./hooks";
import { RESOURCE_IDENTIFIER_EXPLAINER } from "./copy";
import { RemoveTunneledMcpDialogContent } from "./RemoveTunneledMcpDialog";
import { TunneledMcpSetupTabs } from "./TunneledMcpSetupTabs";

const VALID_TABS = ["overview", "setup", "mcp-servers", "settings"] as const;
type TabValue = (typeof VALID_TABS)[number];

function isValidTab(value: string): value is TabValue {
  return (VALID_TABS as readonly string[]).includes(value);
}

export default function TunneledMCPDetails(): JSX.Element | null {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isTunneledMcpEnabled = telemetry.isFeatureEnabled(
    TUNNELED_MCP_FEATURE_FLAG,
  );

  if (isTunneledMcpEnabled === undefined) {
    return null;
  }

  if (!isTunneledMcpEnabled) {
    return <Navigate to={routes.sources.href()} replace />;
  }

  return <TunneledMCPDetailsContent />;
}

function TunneledMCPDetailsContent(): JSX.Element {
  const { sourceSlug } = useParams<{ sourceSlug: string }>();
  const routes = useRoutes();
  const id = sourceSlug ?? "";

  const [activeTab, setActiveTab] = useState<TabValue>(() => {
    const hash = window.location.hash.replace("#", "");
    return isValidTab(hash) ? hash : "overview";
  });

  const handleTabChange = (value: string) => {
    if (!isValidTab(value)) return;
    setActiveTab(value);
    const url = new URL(window.location.href);
    url.hash = value;
    window.history.replaceState(null, "", url.toString());
  };

  const {
    data: tunneledMcpServer,
    isLoading,
    isError,
  } = useGetTunneledMcpServer(getTunneledMcpServerArgs(id), undefined, {
    enabled: id !== "",
  });

  const tunneledMcpServerId = tunneledMcpServer?.id ?? "";
  const {
    data: tunneledMcpServerConnections,
    isLoading: isLoadingConnections,
  } = useListTunneledMcpServerConnections(
    getTunneledMcpServerArgs(tunneledMcpServerId),
    undefined,
    {
      enabled: tunneledMcpServerId !== "",
    },
  );

  const { data: mcpServersResult, isLoading: isLoadingMcpServers } =
    useMcpServers({ tunneledMcpServerId }, undefined, {
      enabled: tunneledMcpServerId !== "",
    });
  const linkedMcpServers = useMcpServersForTunneled(
    mcpServersResult?.mcpServers,
    tunneledMcpServerId,
  );

  const { data: endpointsResult } = useMcpEndpoints({}, undefined, {
    enabled: tunneledMcpServerId !== "",
  });
  const endpointCountByServerId = useMemo(() => {
    const counts = new Map<string, number>();
    for (const endpoint of endpointsResult?.mcpEndpoints ?? []) {
      // Meta-MCP-backed endpoints have no generic server to count against.
      if (!endpoint.mcpServerId) continue;
      counts.set(
        endpoint.mcpServerId,
        (counts.get(endpoint.mcpServerId) ?? 0) + 1,
      );
    }
    return counts;
  }, [endpointsResult]);

  if (isError || (!isLoading && !tunneledMcpServer)) {
    return <Navigate to={routes.sources.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [id]: tunneledMcpServer
              ? formatTunneledMcpDisplay(tunneledMcpServer)
              : undefined,
          }}
          skipSegments={["tunneledmcp"]}
        />
      </Page.Header>

      <Page.Body
        fullWidth
        noPadding
        fullHeight
        overflowHidden
        className="gap-0"
      >
        <TunneledMcpHero server={tunneledMcpServer} />

        <Tabs
          value={activeTab}
          onValueChange={handleTabChange}
          className="flex min-h-0 w-full flex-1 flex-col"
        >
          <div className="shrink-0 border-b">
            <div className="mx-auto max-w-[1270px] px-8">
              <PageTabsList className="h-auto gap-6 bg-transparent p-0">
                <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
                <PageTabsTrigger value="setup">Setup</PageTabsTrigger>
                <PageTabsTrigger value="mcp-servers">
                  MCP Servers
                  {linkedMcpServers.length > 0 &&
                    ` (${linkedMcpServers.length})`}
                </PageTabsTrigger>
                <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
              </PageTabsList>
            </div>
          </div>

          <TabsContent
            value="overview"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <OverviewTab
              tunneledMcpServer={tunneledMcpServer}
              tunneledMcpServerConnections={tunneledMcpServerConnections}
              linkedMcpServersCount={linkedMcpServers.length}
              isLoadingMcpServers={isLoadingMcpServers}
              isLoadingConnections={isLoadingConnections}
              onShowLinkedMcpServers={() => handleTabChange("mcp-servers")}
            />
          </TabsContent>

          <TabsContent
            value="setup"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <div className="mx-auto w-full max-w-[1500px] px-8 py-8">
              <TunneledMcpSetupTabs
                serverName={tunneledMcpServer?.name}
                keyPrefix={tunneledMcpServer?.keyPrefix}
              />
            </div>
          </TabsContent>

          <TabsContent
            value="mcp-servers"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <McpServersTab
              isLoading={isLoadingMcpServers}
              mcpServers={linkedMcpServers}
              endpointCountByServerId={endpointCountByServerId}
              tunneledMcpServer={tunneledMcpServer}
            />
          </TabsContent>

          <TabsContent
            value="settings"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            {tunneledMcpServer && (
              <SettingsTab
                tunneledMcpServer={tunneledMcpServer}
                linkedMcpServers={linkedMcpServers}
              />
            )}
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

function useMcpServersForTunneled(
  servers: McpServer[] | undefined,
  tunneledMcpServerId: string,
) {
  return useMemo(() => {
    if (!servers || !tunneledMcpServerId) return [];
    return servers.filter(
      (server) => server.tunneledMcpServerId === tunneledMcpServerId,
    );
  }, [servers, tunneledMcpServerId]);
}

function TunneledMcpHero({
  server,
}: {
  server: TunneledMcpServer | undefined;
}) {
  return (
    <DetailHero>
      <Stack gap={2}>
        <Page.Eyebrow />
        <Stack direction="horizontal" gap={3} align="center">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center bg-cyan-500/10 dark:bg-cyan-500/20">
            <Network className="h-5 w-5 text-cyan-700 dark:text-cyan-300" />
          </div>
          <Heading variant="h1" className="break-all normal-case">
            {server ? formatTunneledMcpDisplay(server) : "Tunneled MCP server"}
          </Heading>
          <Badge variant="neutral">
            <Badge.Text>Tunneled MCP</Badge.Text>
          </Badge>
          {server && <ConnectionStatusBadge server={server} />}
        </Stack>
      </Stack>
    </DetailHero>
  );
}

function ConnectionStatusBadge({ server }: { server: TunneledMcpServer }) {
  const label =
    server.connectionStatus === "connected"
      ? "Connected"
      : server.connectionStatus === "never_connected"
        ? "Never connected"
        : "Inactive";
  return (
    <Badge variant="neutral">
      <Badge.Text>{label}</Badge.Text>
    </Badge>
  );
}

function OverviewTab({
  tunneledMcpServer,
  tunneledMcpServerConnections,
  linkedMcpServersCount,
  isLoadingMcpServers,
  isLoadingConnections,
  onShowLinkedMcpServers,
}: {
  tunneledMcpServer: TunneledMcpServer | undefined;
  tunneledMcpServerConnections: TunneledMcpServerConnections | undefined;
  linkedMcpServersCount: number;
  isLoadingMcpServers: boolean;
  isLoadingConnections: boolean;
  onShowLinkedMcpServers: () => void;
}) {
  const createdAt = tunneledMcpServer?.createdAt
    ? dateTimeFormatters.humanize(new Date(tunneledMcpServer.createdAt))
    : "-";
  const updatedAt = tunneledMcpServer?.updatedAt
    ? formatDistanceToNow(new Date(tunneledMcpServer.updatedAt), {
        addSuffix: true,
      })
    : "-";
  const lastSeenAt = tunneledMcpServer?.lastSeenAt
    ? formatDistanceToNow(new Date(tunneledMcpServer.lastSeenAt), {
        addSuffix: true,
      })
    : "-";
  const showLinkedCount = tunneledMcpServer != null && !isLoadingMcpServers;

  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      <div className="grid grid-cols-1 items-start gap-8 xl:grid-cols-[320px_1fr]">
        <div className="flex flex-col">
          <Heading variant="h4" className="mb-3">
            Source Information
          </Heading>
          <SourceInfoTable>
            <SourceInfoRow label="Display name">
              <Text className="font-medium">
                {tunneledMcpServer?.name || "-"}
              </Text>
            </SourceInfoRow>
            <SourceInfoRow label="Lifecycle">
              <Text className="font-mono text-sm">
                {tunneledMcpServer?.status ?? "-"}
              </Text>
            </SourceInfoRow>
            <SourceInfoRow label="Connection">
              <Text className="font-mono text-sm">
                {tunneledMcpServer?.connectionStatus ?? "-"}
              </Text>
            </SourceInfoRow>
            <SourceInfoRow label="Key prefix">
              <Text className="font-mono text-sm">
                {tunneledMcpServer?.keyPrefix ?? "-"}
              </Text>
            </SourceInfoRow>
            <SourceInfoRow label="Source ID">
              <span className="flex items-center justify-end gap-1">
                <Text className="font-mono text-sm">
                  {tunneledMcpServer?.id
                    ? `${tunneledMcpServer.id.slice(0, 8)}...`
                    : "-"}
                </Text>
                {tunneledMcpServer?.id && (
                  <CopyButton text={tunneledMcpServer.id} size="xs" />
                )}
              </span>
            </SourceInfoRow>
            <SourceInfoRow label="Last seen">
              <Text className="text-sm">{lastSeenAt}</Text>
            </SourceInfoRow>
            <SourceInfoRow label="Created">
              <Text className="text-sm">{createdAt}</Text>
            </SourceInfoRow>
            <SourceInfoRow label="Updated">
              <Text className="text-sm">{updatedAt}</Text>
            </SourceInfoRow>
            <SourceInfoRow label="Linked MCP servers">
              {showLinkedCount ? (
                <button
                  type="button"
                  onClick={onShowLinkedMcpServers}
                  className="text-primary text-sm hover:underline"
                >
                  {linkedMcpServersCount}
                </button>
              ) : (
                <Text className="text-muted-foreground text-sm">-</Text>
              )}
            </SourceInfoRow>
          </SourceInfoTable>
        </div>

        <div className="grid grid-cols-1 gap-6">
          <ConnectionsPanel
            connections={tunneledMcpServerConnections}
            isLoading={isLoadingConnections}
          />
        </div>
      </div>
    </div>
  );
}

function ConnectionsPanel({
  connections: connectionResult,
  isLoading,
}: {
  connections: TunneledMcpServerConnections | undefined;
  isLoading: boolean;
}) {
  const connections = connectionResult?.connections ?? [];
  const activeConnectionCount = connectionResult?.activeConnectionCount ?? 0;
  const activeConsumerSessionCount =
    connectionResult?.activeConsumerSessionCount ?? 0;

  return (
    <section className="border p-6">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <Heading variant="h4">Connections</Heading>
          <Text muted small>
            Live tunnel sessions from Redis
          </Text>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge variant="neutral">
            <Badge.Text>{activeConnectionCount} active</Badge.Text>
          </Badge>
          <Badge variant="neutral">
            <Badge.Text>
              {activeConsumerSessionCount} consumer sessions
            </Badge.Text>
          </Badge>
        </div>
      </div>

      {isLoading ? (
        <div className="border border-dashed p-6 text-center">
          <Loader2 className="text-muted-foreground mx-auto mb-2 size-4 animate-spin" />
          <Text muted>Loading live tunnel connections.</Text>
        </div>
      ) : connections.length === 0 ? (
        <div className="border border-dashed p-6 text-center">
          <Text muted>No live tunnel connections.</Text>
        </div>
      ) : (
        <div className="grid gap-3">
          {connections.map((connection) => (
            <ConnectionCard
              key={connection.gatewaySessionId}
              connection={connection}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function ConnectionCard({ connection }: { connection: TunneledMcpConnection }) {
  const connectedAt = dateTimeFormatters.humanize(
    new Date(connection.connectedAt),
  );
  const heartbeat = formatDistanceToNow(new Date(connection.lastHeartbeatAt), {
    addSuffix: true,
  });
  const metadataEntries = Object.entries(connection.metadata ?? {});

  return (
    <div className="border p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <Text className="truncate text-sm font-medium">Tunnel agent</Text>
          <Text muted small mono className="mt-1 truncate">
            {connection.gatewaySessionId}
          </Text>
        </div>
        <Badge variant="neutral">
          <Badge.Text>
            {connection.activeConsumerSessions} consumer sessions
          </Badge.Text>
        </Badge>
      </div>
      <div className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm md:grid-cols-2">
        <InfoPair
          label="Service version"
          value={connection.serviceVersion}
          mono
        />
        <InfoPair label="Connected" value={connectedAt} />
        <InfoPair label="Heartbeat" value={heartbeat} />
        <InfoPair label="Agent" value={connection.agentVersion ?? "-"} mono />
        <InfoPair
          label="Remote addr"
          value={connection.remoteAddr ?? "-"}
          mono
        />
        <InfoPair
          label="Request streams"
          value={String(connection.activeSubstreams)}
        />
      </div>
      {metadataEntries.length > 0 && (
        <div className="mt-4 border-t pt-3">
          <Text muted small className="mb-2">
            Metadata
          </Text>
          <div className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm md:grid-cols-2">
            {metadataEntries.map(([key, value]) => (
              <InfoPair key={key} label={key} value={value} mono />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function InfoPair({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex min-w-0 items-center justify-between gap-3">
      <Text muted small>
        {label}
      </Text>
      <Text small mono={mono} className="truncate text-right">
        {value}
      </Text>
    </div>
  );
}

function McpServersTab({
  isLoading,
  mcpServers,
  endpointCountByServerId,
  tunneledMcpServer,
}: {
  isLoading: boolean;
  mcpServers: McpServer[];
  endpointCountByServerId: Map<string, number>;
  tunneledMcpServer: TunneledMcpServer | undefined;
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      {isLoading ? (
        <McpServersSkeleton />
      ) : mcpServers.length > 0 ? (
        <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
          {mcpServers.map((server) => (
            <MCPServerCard
              key={server.id}
              server={server}
              endpointCount={endpointCountByServerId.get(server.id) ?? 0}
            />
          ))}
        </div>
      ) : (
        <McpServersEmptyState tunneledMcpServer={tunneledMcpServer} />
      )}
    </div>
  );
}

function McpServersEmptyState({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer | undefined;
}) {
  const link = useLinkMcpServerToTunneled();

  const handleAdd = async () => {
    if (!tunneledMcpServer) return;
    try {
      await link.mutateAsync({ tunneledMcpServer });
      toast.success("MCP server added");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to add MCP server";
      toast.error(message);
    }
  };

  return (
    <div className="flex flex-col items-center py-12 text-center">
      <Server className="text-muted-foreground/50 mb-3 h-12 w-12" />
      <Text muted className="mb-4">
        No MCP servers are linked to this source yet.
      </Text>
      <RequireScope scope="mcp:write" level="component">
        <Button
          variant="primary"
          disabled={!tunneledMcpServer || link.isPending}
          onClick={() => void handleAdd()}
        >
          <Button.LeftIcon>
            {link.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Plus className="size-4" />
            )}
          </Button.LeftIcon>
          <Button.Text>
            {link.isPending ? "Adding" : "Add MCP server"}
          </Button.Text>
        </Button>
      </RequireScope>
    </div>
  );
}

function McpServersSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      {[1, 2, 3].map((i) => (
        <div key={i} className="bg-card animate-pulse border p-6">
          <div className="mb-4 flex items-center gap-3">
            <div className="bg-muted h-10 w-10" />
            <div className="flex-1">
              <div className="bg-muted mb-2 h-4 w-24" />
              <div className="bg-muted h-3 w-32" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

function SettingsTab({
  tunneledMcpServer,
  linkedMcpServers,
}: {
  tunneledMcpServer: TunneledMcpServer;
  linkedMcpServers: McpServer[];
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <NameSection tunneledMcpServer={tunneledMcpServer} />
      <ResourceIdentifierSection tunneledMcpServer={tunneledMcpServer} />
      <PublicAccessSection tunneledMcpServer={tunneledMcpServer} />
      <TunnelKeySection tunneledMcpServer={tunneledMcpServer} />
      <DangerZoneSection
        tunneledMcpServer={tunneledMcpServer}
        linkedMcpServers={linkedMcpServers}
      />
    </div>
  );
}

async function invalidateTunneledMcpServerViews(queryClient: QueryClient) {
  await Promise.all([
    invalidateAllGetTunneledMcpServer(queryClient, { refetchType: "all" }),
    invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
  ]);
}

const PUBLIC_ACCESS_CONFIRM_PHRASE = "ALLOW PUBLIC ACCESS";

function PublicAccessSection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}) {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmPhrase, setConfirmPhrase] = useState("");

  const allowPublic = tunneledMcpServer.allowPublic;

  // Rethrow so a confirmation dialog can stay open on failure; a toast still
  // surfaces the error.
  const applyAllowPublic = async (next: boolean) => {
    try {
      await update.mutateAsync({
        request: {
          updateTunneledMcpServerForm: {
            id: tunneledMcpServer.id,
            allowPublic: next,
          },
        },
      });
      await invalidateTunneledMcpServerViews(queryClient);
      toast.success(
        next
          ? "Public anonymous access enabled"
          : "Public anonymous access disabled",
      );
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : "Failed to update public access";
      toast.error(message);
      throw error instanceof Error ? error : new Error(message);
    }
  };

  const handleConfirmEnable = async () => {
    try {
      await applyAllowPublic(true);
    } catch {
      // Keep the dialog open on failure so the user can retry; the error
      // Alert inside it and the toast already surface the reason.
      return;
    }
    setConfirmOpen(false);
    setConfirmPhrase("");
  };

  const closeConfirm = (open: boolean) => {
    setConfirmOpen(open);
    if (!open) {
      setConfirmPhrase("");
      update.reset();
    }
  };

  return (
    <div className="border p-6">
      <div className="mb-1 flex items-center gap-2">
        <Text variant="subheading">Public Access</Text>
        <ReleaseStageBadge stage="preview" />
      </div>
      <Text muted small className="mb-4 max-w-3xl">
        When enabled, MCP servers fronting this source can be published with{" "}
        <span className="font-medium">public</span> visibility, serving fully
        anonymous callers with no login. Anyone who can reach the endpoint URL
        can call every tool this source exposes. Leave this off unless the
        upstream MCP server is safe to expose to the public internet.
      </Text>

      {allowPublic ? (
        <Stack gap={3}>
          <Alert variant="warning" dismissible={false}>
            Public anonymous access is enabled. MCP servers fronting this source
            may serve unauthenticated callers.
          </Alert>
          <RequireScope scope="mcp:write" level="component">
            <Button
              variant="secondary"
              disabled={update.isPending}
              onClick={() => void applyAllowPublic(false)}
            >
              {update.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Disabling</Button.Text>
                </>
              ) : (
                <Button.Text>Disable public access</Button.Text>
              )}
            </Button>
          </RequireScope>
        </Stack>
      ) : (
        <RequireScope scope="mcp:write" level="component">
          <Button
            variant="secondary"
            disabled={update.isPending}
            onClick={() => setConfirmOpen(true)}
          >
            <Button.Text>Enable public access</Button.Text>
          </Button>
        </RequireScope>
      )}

      <Dialog open={confirmOpen} onOpenChange={closeConfirm}>
        <Dialog.Content className="max-w-xl!">
          <Dialog.Header>
            <Dialog.Title>Enable public anonymous access?</Dialog.Title>
            <Dialog.Description>
              This lets anyone on the internet call this source's tools with no
              authentication once an MCP server fronting it is set to public
              visibility.
            </Dialog.Description>
          </Dialog.Header>
          <Alert variant="warning" dismissible={false}>
            Only enable this for MCP servers that are safe to expose publicly.
            Every tool, resource, and prompt becomes reachable without a login.
          </Alert>
          <Stack gap={2}>
            <Text small muted>
              Type{" "}
              <span className="font-mono font-medium">
                {PUBLIC_ACCESS_CONFIRM_PHRASE}
              </span>{" "}
              to confirm.
            </Text>
            <Input
              value={confirmPhrase}
              onChange={(value) => setConfirmPhrase(value)}
              placeholder={PUBLIC_ACCESS_CONFIRM_PHRASE}
            />
            {update.isError && (
              <Alert variant="error" dismissible={false}>
                {update.error.message}
              </Alert>
            )}
          </Stack>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={() => closeConfirm(false)}
              disabled={update.isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={
                confirmPhrase.trim() !== PUBLIC_ACCESS_CONFIRM_PHRASE ||
                update.isPending
              }
              onClick={() => void handleConfirmEnable()}
            >
              {update.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Enabling</Button.Text>
                </>
              ) : (
                <Button.Text>Enable public access</Button.Text>
              )}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

// One bordered settings section editing a single text field of the tunneled
// server, owning its own draft, error, and pending state so sibling sections
// never reflect each other's activity.
function EditableFieldSection({
  id,
  title,
  description,
  placeholder,
  stored,
  requireValue = false,
  save,
  toastMessage,
  fallbackError,
}: {
  id: string;
  title: string;
  description: ReactNode;
  placeholder: string;
  stored: string;
  requireValue?: boolean;
  // Persists the trimmed draft and returns the canonical stored value.
  save: (value: string) => Promise<string>;
  toastMessage: (cleared: boolean) => string;
  fallbackError: string;
}) {
  const [draft, setDraft] = useState(stored);
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setDraft(stored);
  }, [stored]);

  const dirty = draft.trim() !== stored.trim();
  const saveDisabled =
    !dirty || (requireValue && draft.trim() === "") || saving;

  const handleSave = async () => {
    const value = draft.trim();
    setSaving(true);
    setError(undefined);
    try {
      // Adopt the stored form the server normalized to. Refetching alone
      // leaves the draft dirty whenever normalization is a no-op server-side.
      setDraft(await save(value));
      setError(undefined);
      toast.success(toastMessage(value === ""));
    } catch (err) {
      const message = err instanceof Error ? err.message : fallbackError;
      setError(message);
      toast.error(message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="border p-6">
      <Text variant="subheading" id={`${id}-label`} className="mb-1">
        {title}
      </Text>
      <Text muted small className="mb-4 max-w-3xl">
        {description}
      </Text>
      <Stack gap={2}>
        <Field data-invalid={error !== undefined ? true : undefined}>
          <Input
            id={id}
            aria-labelledby={`${id}-label`}
            value={draft}
            onChange={(value) => setDraft(value)}
            placeholder={placeholder}
            disabled={saving}
            aria-invalid={error !== undefined}
          />
          {error !== undefined && <FieldError>{error}</FieldError>}
        </Field>
        <Stack direction="horizontal" gap={2}>
          <RequireScope scope="mcp:write" level="component">
            <Button
              variant="primary"
              disabled={saveDisabled}
              onClick={() => void handleSave()}
            >
              {saving ? (
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
              ) : null}
              <Button.Text>{saving ? "Saving" : "Save"}</Button.Text>
            </Button>
          </RequireScope>
        </Stack>
      </Stack>
    </div>
  );
}

function NameSection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}) {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();

  return (
    <EditableFieldSection
      id="tunneled-mcp-display-name"
      title="Display Name"
      description="Name shown in source listings, breadcrumbs, and linked MCP servers."
      placeholder="Internal MCP server"
      stored={tunneledMcpServer.name}
      requireValue
      save={async (value) => {
        const saved = await update.mutateAsync({
          request: {
            updateTunneledMcpServerForm: {
              id: tunneledMcpServer.id,
              name: value,
            },
          },
        });
        await invalidateTunneledMcpServerViews(queryClient);
        return saved.name;
      }}
      toastMessage={() => "Tunneled MCP name updated"}
      fallbackError="Failed to update name"
    />
  );
}

function ResourceIdentifierSection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}) {
  const update = useUpdateTunneledMcpServerMutation();
  const queryClient = useQueryClient();

  return (
    <EditableFieldSection
      id="tunneled-mcp-resource-identifier"
      title="Resource Identifier"
      description={`${RESOURCE_IDENTIFIER_EXPLAINER} Leave blank if the server has no OAuth of its own.`}
      placeholder="https://mcp.internal.example.com/mcp"
      stored={tunneledMcpServer.resourceIdentifier ?? ""}
      save={async (value) => {
        // An empty string clears the identifier back to unset.
        const saved = await update.mutateAsync({
          request: {
            updateTunneledMcpServerForm: {
              id: tunneledMcpServer.id,
              resourceIdentifier: value,
            },
          },
        });
        await invalidateTunneledMcpServerViews(queryClient);
        return saved.resourceIdentifier ?? "";
      }}
      toastMessage={(cleared) =>
        cleared ? "Resource identifier cleared" : "Resource identifier updated"
      }
      fallbackError="Failed to update resource identifier"
    />
  );
}

function TunnelKeySection({
  tunneledMcpServer,
}: {
  tunneledMcpServer: TunneledMcpServer;
}) {
  const [rotateDialogOpen, setRotateDialogOpen] = useState(false);
  const [rotatedKey, setRotatedKey] =
    useState<RotateTunneledMcpServerKeyData>();
  const rotate = useRotateTunneledMcpServerKey();

  const handleOpenChange = (open: boolean) => {
    setRotateDialogOpen(open);
    if (!open) {
      setRotatedKey(undefined);
      rotate.reset();
    }
  };

  const handleRotate = async () => {
    try {
      const result = await rotate.mutateAsync({
        tunneledMcpServerId: tunneledMcpServer.id,
      });
      setRotatedKey(result);
      toast.success("Tunnel key rotated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to rotate tunnel key";
      toast.error(message);
    }
  };

  return (
    <div className="border p-6">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div>
          <Text variant="subheading" className="mb-1">
            Tunnel Key
          </Text>
          <Text muted small>
            Current key prefix:{" "}
            <span className="font-mono">{tunneledMcpServer.keyPrefix}</span>
          </Text>
        </div>
        <RequireScope scope="mcp:write" level="component">
          <Button
            variant="secondary"
            size="md"
            onClick={() => setRotateDialogOpen(true)}
          >
            <Button.LeftIcon>
              <RotateCcw className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Rotate</Button.Text>
          </Button>
        </RequireScope>
      </div>
      <Text muted small>
        Rotation replaces the key used by tunnel agents for this source.
      </Text>

      <Dialog open={rotateDialogOpen} onOpenChange={handleOpenChange}>
        <Dialog.Content className="max-w-xl!">
          {rotatedKey ? (
            <>
              <Dialog.Header>
                <Dialog.Title>Tunnel Key Rotated</Dialog.Title>
                <Dialog.Description>
                  Copy the new key now. It will not be shown again.
                </Dialog.Description>
              </Dialog.Header>
              <Alert variant="warning" dismissible={false}>
                Restart tunnel agents with the new key to reconnect this source.
              </Alert>
              <div className="bg-muted flex items-center gap-2 p-3">
                <code className="min-w-0 flex-1 break-all text-sm">
                  {rotatedKey.tunnelKey}
                </code>
                <CopyButton
                  text={rotatedKey.tunnelKey}
                  size="sm"
                  tooltip="Copy tunnel key"
                />
              </div>
              <div className="flex items-center gap-2">
                <KeyRound className="text-muted-foreground h-4 w-4" />
                <Text small muted>
                  Prefix: {rotatedKey.tunneledMcpServer.keyPrefix}
                </Text>
              </div>
              <Dialog.Footer>
                <Button onClick={() => handleOpenChange(false)}>
                  <Button.Text>Close</Button.Text>
                </Button>
              </Dialog.Footer>
            </>
          ) : (
            <>
              <Dialog.Header>
                <Dialog.Title>Rotate Tunnel Key</Dialog.Title>
                <Dialog.Description>
                  The current key will stop working for new tunnel connections.
                </Dialog.Description>
              </Dialog.Header>
              <Alert variant="warning" dismissible={false}>
                Running agents using the old key will be disconnected shortly
                and must be restarted with the replacement key.
              </Alert>
              {rotate.isError && (
                <Alert variant="error" dismissible={false}>
                  {rotate.error.message}
                </Alert>
              )}
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => handleOpenChange(false)}
                  disabled={rotate.isPending}
                >
                  <Button.Text>Cancel</Button.Text>
                </Button>
                <Button
                  variant="destructive-primary"
                  onClick={() => void handleRotate()}
                  disabled={rotate.isPending}
                >
                  {rotate.isPending ? (
                    <Button.LeftIcon>
                      <Loader2 className="size-4 animate-spin" />
                    </Button.LeftIcon>
                  ) : null}
                  <Button.Text>
                    {rotate.isPending ? "Rotating" : "Rotate"}
                  </Button.Text>
                </Button>
              </Dialog.Footer>
            </>
          )}
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

function DangerZoneSection({
  tunneledMcpServer,
  linkedMcpServers,
}: {
  tunneledMcpServer: TunneledMcpServer;
  linkedMcpServers: McpServer[];
}) {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const displayName = formatTunneledMcpDisplay(tunneledMcpServer);

  return (
    <div className="border-destructive/30 border p-6">
      <Text variant="subheading" className="text-destructive mb-1">
        Danger Zone
      </Text>
      <Text muted small className="mb-4">
        Deleting this source will also remove the linked MCP servers and their
        endpoints. This action cannot be undone.
      </Text>
      <RequireScope scope="mcp:write" level="component">
        <Button
          variant="destructive-primary"
          size="md"
          onClick={() => setDeleteDialogOpen(true)}
        >
          <Button.LeftIcon>
            <Trash2 className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Delete Source</Button.Text>
        </Button>
      </RequireScope>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <Dialog.Content className="max-w-2xl!">
          <RemoveTunneledMcpDialogContent
            tunneledMcpServerId={tunneledMcpServer.id}
            displayName={displayName}
            linkedMcpServers={linkedMcpServers}
            onClose={() => setDeleteDialogOpen(false)}
            onSuccess={() => {
              setDeleteDialogOpen(false);
              void navigate(routes.sources.href());
            }}
          />
        </Dialog.Content>
      </Dialog>
    </div>
  );
}
