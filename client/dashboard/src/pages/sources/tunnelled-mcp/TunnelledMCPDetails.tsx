import { CodeBlock } from "@/components/code";
import { DetailHero } from "@/components/detail-hero";
import { MCPServerCard } from "@/components/mcp/MCPServerCard";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import {
  SourceInfoRow,
  SourceInfoTable,
} from "@/components/sources/SourceInfoTable";
import { CopyButton } from "@/components/ui/copy-button";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { dateTimeFormatters } from "@/lib/dates";
import {
  formatTunnelledMcpDisplay,
  getTunnelledMcpServerArgs,
} from "@/lib/sources";
import { useRoutes } from "@/routes";
import type {
  McpServer,
  TunnelledMcpConnection,
  TunnelledMcpServer,
} from "@gram/client/models/components";
import {
  invalidateAllGetTunnelledMcpServer,
  invalidateAllTunnelledMcpServers,
  useGetTunnelledMcpServer,
  useMcpEndpoints,
  useMcpServers,
  useUpdateTunnelledMcpServerMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Badge, Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { formatDistanceToNow } from "date-fns";
import { Loader2, Network, Plus, Server, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Navigate, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { useLinkMcpServerToTunnelled } from "./hooks";
import { RemoveTunnelledMcpDialogContent } from "./RemoveTunnelledMcpDialog";

const VALID_TABS = ["overview", "setup", "mcp-servers", "settings"] as const;
type TabValue = (typeof VALID_TABS)[number];

function isValidTab(value: string): value is TabValue {
  return (VALID_TABS as readonly string[]).includes(value);
}

export default function TunnelledMCPDetails(): JSX.Element {
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
    data: tunnelledMcpServer,
    isLoading,
    isError,
  } = useGetTunnelledMcpServer(getTunnelledMcpServerArgs(id), undefined, {
    enabled: id !== "",
  });

  const tunnelledMcpServerId = tunnelledMcpServer?.id ?? "";

  const { data: mcpServersResult, isLoading: isLoadingMcpServers } =
    useMcpServers({ tunnelledMcpServerId }, undefined, {
      enabled: tunnelledMcpServerId !== "",
    });
  const linkedMcpServers = useMcpServersForTunnelled(
    mcpServersResult?.mcpServers,
    tunnelledMcpServerId,
  );

  const { data: endpointsResult } = useMcpEndpoints({}, undefined, {
    enabled: tunnelledMcpServerId !== "",
  });
  const endpointCountByServerId = useMemo(() => {
    const counts = new Map<string, number>();
    for (const endpoint of endpointsResult?.mcpEndpoints ?? []) {
      counts.set(
        endpoint.mcpServerId,
        (counts.get(endpoint.mcpServerId) ?? 0) + 1,
      );
    }
    return counts;
  }, [endpointsResult]);

  if (isError || (!isLoading && !tunnelledMcpServer)) {
    return <Navigate to={routes.sources.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [id]: tunnelledMcpServer
              ? formatTunnelledMcpDisplay(tunnelledMcpServer)
              : undefined,
          }}
          skipSegments={["tunnelledmcp"]}
        />
      </Page.Header>

      <Page.Body
        fullWidth
        noPadding
        fullHeight
        overflowHidden
        className="gap-0"
      >
        <TunnelledMcpHero server={tunnelledMcpServer} />

        <Tabs
          value={activeTab}
          onValueChange={handleTabChange}
          className="flex min-h-0 w-full flex-1 flex-col"
        >
          <div className="shrink-0 border-b">
            <div className="mx-auto max-w-[1270px] px-8">
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
                <PageTabsTrigger value="setup">Setup</PageTabsTrigger>
                <PageTabsTrigger value="mcp-servers">
                  MCP Servers
                  {linkedMcpServers.length > 0 &&
                    ` (${linkedMcpServers.length})`}
                </PageTabsTrigger>
                <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
              </TabsList>
            </div>
          </div>

          <TabsContent
            value="overview"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <OverviewTab
              tunnelledMcpServer={tunnelledMcpServer}
              linkedMcpServersCount={linkedMcpServers.length}
              isLoadingMcpServers={isLoadingMcpServers}
              onShowLinkedMcpServers={() => handleTabChange("mcp-servers")}
            />
          </TabsContent>

          <TabsContent
            value="setup"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
              <TunnelledMcpSetupTabs
                serverName={tunnelledMcpServer?.name}
                keyPrefix={tunnelledMcpServer?.keyPrefix}
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
              tunnelledMcpServer={tunnelledMcpServer}
            />
          </TabsContent>

          <TabsContent
            value="settings"
            className="mt-0 min-h-0 flex-1 overflow-y-auto"
          >
            {tunnelledMcpServer && (
              <SettingsTab
                tunnelledMcpServer={tunnelledMcpServer}
                linkedMcpServers={linkedMcpServers}
              />
            )}
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

function useMcpServersForTunnelled(
  servers: McpServer[] | undefined,
  tunnelledMcpServerId: string,
) {
  return useMemo(() => {
    if (!servers || !tunnelledMcpServerId) return [];
    return servers.filter(
      (server) => server.tunnelledMcpServerId === tunnelledMcpServerId,
    );
  }, [servers, tunnelledMcpServerId]);
}

function TunnelledMcpHero({
  server,
}: {
  server: TunnelledMcpServer | undefined;
}) {
  return (
    <DetailHero>
      <Stack gap={2}>
        <Stack direction="horizontal" gap={3} align="center">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-cyan-500/10 dark:bg-cyan-500/20">
            <Network className="h-5 w-5 text-cyan-700 dark:text-cyan-300" />
          </div>
          <Heading variant="h1" className="break-all normal-case">
            {server
              ? formatTunnelledMcpDisplay(server)
              : "Tunnelled MCP server"}
          </Heading>
          <Badge variant="neutral">
            <Badge.Text>Tunnelled MCP</Badge.Text>
          </Badge>
          {server && <ConnectionStatusBadge server={server} />}
        </Stack>
      </Stack>
    </DetailHero>
  );
}

function ConnectionStatusBadge({ server }: { server: TunnelledMcpServer }) {
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
  tunnelledMcpServer,
  linkedMcpServersCount,
  isLoadingMcpServers,
  onShowLinkedMcpServers,
}: {
  tunnelledMcpServer: TunnelledMcpServer | undefined;
  linkedMcpServersCount: number;
  isLoadingMcpServers: boolean;
  onShowLinkedMcpServers: () => void;
}) {
  const createdAt = tunnelledMcpServer?.createdAt
    ? dateTimeFormatters.humanize(new Date(tunnelledMcpServer.createdAt))
    : "-";
  const updatedAt = tunnelledMcpServer?.updatedAt
    ? formatDistanceToNow(new Date(tunnelledMcpServer.updatedAt), {
        addSuffix: true,
      })
    : "-";
  const lastSeenAt = tunnelledMcpServer?.lastSeenAt
    ? formatDistanceToNow(new Date(tunnelledMcpServer.lastSeenAt), {
        addSuffix: true,
      })
    : "-";
  const manifestAt = tunnelledMcpServer?.manifestAt
    ? formatDistanceToNow(new Date(tunnelledMcpServer.manifestAt), {
        addSuffix: true,
      })
    : "-";
  const showLinkedCount = tunnelledMcpServer != null && !isLoadingMcpServers;

  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      <div className="grid grid-cols-1 items-start gap-8 xl:grid-cols-[320px_1fr]">
        <div className="flex flex-col">
          <Heading variant="h4" className="mb-3">
            Source Information
          </Heading>
          <SourceInfoTable>
            <SourceInfoRow label="Display name">
              <Type className="font-medium">
                {tunnelledMcpServer?.name || "-"}
              </Type>
            </SourceInfoRow>
            <SourceInfoRow label="Lifecycle">
              <Type className="font-mono text-sm">
                {tunnelledMcpServer?.status ?? "-"}
              </Type>
            </SourceInfoRow>
            <SourceInfoRow label="Connection">
              <Type className="font-mono text-sm">
                {tunnelledMcpServer?.connectionStatus ?? "-"}
              </Type>
            </SourceInfoRow>
            <SourceInfoRow label="Key prefix">
              <Type className="font-mono text-sm">
                {tunnelledMcpServer?.keyPrefix ?? "-"}
              </Type>
            </SourceInfoRow>
            <SourceInfoRow label="Source ID">
              <span className="flex items-center justify-end gap-1">
                <Type className="font-mono text-sm">
                  {tunnelledMcpServer?.id
                    ? `${tunnelledMcpServer.id.slice(0, 8)}...`
                    : "-"}
                </Type>
                {tunnelledMcpServer?.id && (
                  <CopyButton text={tunnelledMcpServer.id} size="inline" />
                )}
              </span>
            </SourceInfoRow>
            <SourceInfoRow label="Last seen">
              <Type className="text-sm">{lastSeenAt}</Type>
            </SourceInfoRow>
            <SourceInfoRow label="Manifest">
              <Type className="text-sm">{manifestAt}</Type>
            </SourceInfoRow>
            <SourceInfoRow label="Created">
              <Type className="text-sm">{createdAt}</Type>
            </SourceInfoRow>
            <SourceInfoRow label="Updated">
              <Type className="text-sm">{updatedAt}</Type>
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
                <Type className="text-muted-foreground text-sm">-</Type>
              )}
            </SourceInfoRow>
          </SourceInfoTable>
        </div>

        <div className="grid grid-cols-1 gap-6">
          <ConnectionsPanel server={tunnelledMcpServer} />
          <ManifestPanel server={tunnelledMcpServer} />
        </div>
      </div>
    </div>
  );
}

function ConnectionsPanel({
  server,
}: {
  server: TunnelledMcpServer | undefined;
}) {
  const connections = server?.connections ?? [];
  const activeConnectionCount =
    server?.activeConnectionCount ?? connections.length;
  const activeConsumerSessionCount =
    server?.activeConsumerSessionCount ??
    connections.reduce(
      (total, connection) => total + connection.activeConsumerSessions,
      0,
    );

  return (
    <section className="rounded-lg border p-6">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <Heading variant="h4">Connections</Heading>
          <Type muted small>
            Live tunnel sessions from Redis
          </Type>
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

      {connections.length === 0 ? (
        <div className="rounded-md border border-dashed p-6 text-center">
          <Type muted>No live tunnel connections.</Type>
        </div>
      ) : (
        <div className="grid gap-3">
          {connections.map((connection) => (
            <ConnectionCard
              key={connection.sessionId}
              connection={connection}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function ConnectionCard({
  connection,
}: {
  connection: TunnelledMcpConnection;
}) {
  const connectedAt = dateTimeFormatters.humanize(
    new Date(connection.connectedAt),
  );
  const heartbeat = formatDistanceToNow(new Date(connection.lastHeartbeatAt), {
    addSuffix: true,
  });
  const metadataEntries = Object.entries(connection.metadata ?? {});

  return (
    <div className="rounded-md border p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0">
          <Type className="truncate font-mono text-sm">
            {connection.serviceSlug}
          </Type>
          <Type muted small mono className="mt-1 truncate">
            {connection.sessionId}
          </Type>
        </div>
        <Badge variant="neutral">
          <Badge.Text>
            {connection.activeConsumerSessions} consumer sessions
          </Badge.Text>
        </Badge>
      </div>
      <div className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm md:grid-cols-2">
        <InfoPair label="Service ID" value={connection.serviceId} mono />
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
          <Type muted small className="mb-2">
            Metadata
          </Type>
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

function ManifestPanel({ server }: { server: TunnelledMcpServer | undefined }) {
  const tools = server?.manifest.tools ?? [];
  const resources = server?.manifest.resources ?? [];

  return (
    <section className="rounded-lg border p-6">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div>
          <Heading variant="h4">Manifest Snapshot</Heading>
          <Type muted small>
            Last-known tools and resources from the tunnelled MCP server
          </Type>
        </div>
        <div className="flex gap-2">
          <Badge variant="neutral">
            <Badge.Text>{tools.length} tools</Badge.Text>
          </Badge>
          <Badge variant="neutral">
            <Badge.Text>{resources.length} resources</Badge.Text>
          </Badge>
        </div>
      </div>

      {tools.length === 0 && resources.length === 0 ? (
        <div className="rounded-md border border-dashed p-6 text-center">
          <Type muted>No manifest captured yet.</Type>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <ManifestList
            title="Tools"
            empty="No tools advertised"
            items={tools.map((tool) => ({
              key: tool.name,
              title: tool.name,
              description: tool.description,
            }))}
          />
          <ManifestList
            title="Resources"
            empty="No resources advertised"
            items={resources.map((resource) => ({
              key: resource.uri,
              title: resource.name || resource.uri,
              description: resource.uri,
            }))}
          />
        </div>
      )}
    </section>
  );
}

function ManifestList({
  title,
  empty,
  items,
}: {
  title: string;
  empty: string;
  items: Array<{ key: string; title: string; description?: string }>;
}) {
  return (
    <div>
      <Type variant="subheading" className="mb-2">
        {title}
      </Type>
      {items.length === 0 ? (
        <Type muted small>
          {empty}
        </Type>
      ) : (
        <div className="space-y-2">
          {items.slice(0, 6).map((item) => (
            <div key={item.key} className="rounded-md border px-3 py-2">
              <Type small className="font-medium">
                {item.title}
              </Type>
              {item.description && (
                <Type muted small className="mt-1 line-clamp-2">
                  {item.description}
                </Type>
              )}
            </div>
          ))}
          {items.length > 6 && (
            <Type muted small>
              +{items.length - 6} more
            </Type>
          )}
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
      <Type muted small>
        {label}
      </Type>
      <Type small mono={mono} className="truncate text-right">
        {value}
      </Type>
    </div>
  );
}

function McpServersTab({
  isLoading,
  mcpServers,
  endpointCountByServerId,
  tunnelledMcpServer,
}: {
  isLoading: boolean;
  mcpServers: McpServer[];
  endpointCountByServerId: Map<string, number>;
  tunnelledMcpServer: TunnelledMcpServer | undefined;
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
        <McpServersEmptyState tunnelledMcpServer={tunnelledMcpServer} />
      )}
    </div>
  );
}

function McpServersEmptyState({
  tunnelledMcpServer,
}: {
  tunnelledMcpServer: TunnelledMcpServer | undefined;
}) {
  const link = useLinkMcpServerToTunnelled();

  const handleAdd = async () => {
    if (!tunnelledMcpServer) return;
    try {
      await link.mutateAsync({ tunnelledMcpServer });
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
      <Type muted className="mb-4">
        No MCP servers are linked to this source yet.
      </Type>
      <RequireScope scope="mcp:write" level="component">
        <Button
          variant="primary"
          disabled={!tunnelledMcpServer || link.isPending}
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
        <div key={i} className="bg-card animate-pulse rounded-xl border p-6">
          <div className="mb-4 flex items-center gap-3">
            <div className="bg-muted h-10 w-10 rounded-lg" />
            <div className="flex-1">
              <div className="bg-muted mb-2 h-4 w-24 rounded" />
              <div className="bg-muted h-3 w-32 rounded" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

function SettingsTab({
  tunnelledMcpServer,
  linkedMcpServers,
}: {
  tunnelledMcpServer: TunnelledMcpServer;
  linkedMcpServers: McpServer[];
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <NameSection tunnelledMcpServer={tunnelledMcpServer} />
      <DangerZoneSection
        tunnelledMcpServer={tunnelledMcpServer}
        linkedMcpServers={linkedMcpServers}
      />
    </div>
  );
}

function NameSection({
  tunnelledMcpServer,
}: {
  tunnelledMcpServer: TunnelledMcpServer;
}) {
  const [draft, setDraft] = useState(tunnelledMcpServer.name);

  useEffect(() => {
    setDraft(tunnelledMcpServer.name);
  }, [tunnelledMcpServer.name]);

  const queryClient = useQueryClient();
  const update = useUpdateTunnelledMcpServerMutation();

  const dirty = draft.trim() !== tunnelledMcpServer.name.trim();
  const saveDisabled = !dirty || draft.trim() === "" || update.isPending;

  const handleSave = async () => {
    try {
      await update.mutateAsync({
        request: {
          updateTunnelledMcpServerForm: {
            id: tunnelledMcpServer.id,
            name: draft.trim(),
          },
        },
      });
      await Promise.all([
        invalidateAllGetTunnelledMcpServer(queryClient, {
          refetchType: "all",
        }),
        invalidateAllTunnelledMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Tunnelled MCP name updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update name";
      toast.error(message);
    }
  };

  return (
    <div className="rounded-lg border p-6">
      <Type variant="subheading" className="mb-1">
        Display Name
      </Type>
      <Type muted small className="mb-4">
        Name shown in source listings, breadcrumbs, and linked MCP servers.
      </Type>
      <Stack gap={2}>
        <Input
          value={draft}
          onChange={(value) => setDraft(value)}
          placeholder="Internal MCP server"
        />
        {update.isError && (
          <Alert variant="error" dismissible={false}>
            {update.error.message}
          </Alert>
        )}
        <Stack direction="horizontal" gap={2}>
          <RequireScope scope="mcp:write" level="component">
            <Button
              variant="primary"
              disabled={saveDisabled}
              onClick={() => void handleSave()}
            >
              {update.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Saving</Button.Text>
                </>
              ) : (
                <Button.Text>Save</Button.Text>
              )}
            </Button>
          </RequireScope>
        </Stack>
      </Stack>
    </div>
  );
}

function DangerZoneSection({
  tunnelledMcpServer,
  linkedMcpServers,
}: {
  tunnelledMcpServer: TunnelledMcpServer;
  linkedMcpServers: McpServer[];
}) {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const displayName = formatTunnelledMcpDisplay(tunnelledMcpServer);

  return (
    <div className="border-destructive/30 rounded-lg border p-6">
      <Type variant="subheading" className="text-destructive mb-1">
        Danger Zone
      </Type>
      <Type muted small className="mb-4">
        Deleting this source will also remove the linked MCP servers and their
        endpoints. This action cannot be undone.
      </Type>
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
          <RemoveTunnelledMcpDialogContent
            tunnelledMcpServerId={tunnelledMcpServer.id}
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

export function TunnelledMcpSetupTabs({
  tunnelKey,
  keyPrefix,
  serverName,
}: {
  tunnelKey?: string;
  keyPrefix?: string;
  serverName?: string;
}): JSX.Element {
  const renderedKey = tunnelKey ?? "<YOUR_TUNNEL_KEY>";
  const slug = slugForSnippet(serverName);
  const serviceId = `${slug}-service`;
  const serviceVersion = "2026.06.1";
  const upstream = "http://localhost:3000/mcp";
  const clusterUpstream = "http://127.0.0.1:3000/mcp";
  const gateway = "wss://tunnel.getgram.ai/connect";
  const kubernetes = `apiVersion: v1
kind: ConfigMap
metadata:
  name: hello-world-mcp
data:
  server.py: |
    from mcp.server.fastmcp import FastMCP

    mcp = FastMCP(
        "hello-world",
        host="0.0.0.0",
        port=3000,
        stateless_http=True,
        json_response=True,
    )

    @mcp.tool()
    def hello(name: str = "world") -> str:
        """Return a friendly greeting."""
        return f"Hello, {name}!"

    @mcp.resource("hello://world")
    def hello_resource() -> str:
        return "Hello from a tunnelled MCP server."

    if __name__ == "__main__":
        mcp.run(transport="streamable-http")
---
apiVersion: v1
kind: Secret
metadata:
  name: gram-tunnel-key
type: Opaque
stringData:
  GRAM_TUNNEL_KEY: ${yamlQuote(renderedKey)}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gram-tunnel-${slug}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: gram-tunnel-${slug}
  template:
    metadata:
      labels:
        app: gram-tunnel-${slug}
    spec:
      containers:
        - name: hello-world-mcp
          image: python:3.12-slim
          command: ["/bin/sh", "-lc"]
          args:
            - |
              pip install "mcp[cli]>=1.27,<2" &&
              python /app/server.py
          ports:
            - containerPort: 3000
          volumeMounts:
            - name: hello-world-mcp
              mountPath: /app
              readOnly: true
        - name: tunnel-agent
          image: ghcr.io/speakeasy-api/gram-tunnel-agent:latest
          env:
            - name: GRAM_TUNNEL_KEY
              valueFrom:
                secretKeyRef:
                  name: gram-tunnel-key
                  key: GRAM_TUNNEL_KEY
            - name: GRAM_TUNNEL_UPSTREAM
              value: ${yamlQuote(clusterUpstream)}
            - name: GRAM_TUNNEL_ENDPOINT
              value: ${yamlQuote(gateway)}
            - name: GRAM_MCP_SERVICE_ID
              value: ${yamlQuote(serviceId)}
            - name: GRAM_MCP_SERVICE_SLUG
              value: ${yamlQuote(slug)}
            - name: GRAM_MCP_SERVICE_VERSION
              value: ${yamlQuote(serviceVersion)}
      volumes:
        - name: hello-world-mcp
          configMap:
            name: hello-world-mcp`;
  const docker = `docker run --rm --name gram-tunnel-${slug} \\
  -e GRAM_TUNNEL_KEY=${shellQuote(renderedKey)} \\
  -e GRAM_TUNNEL_UPSTREAM=${shellQuote("http://host.docker.internal:3000/mcp")} \\
  -e GRAM_TUNNEL_ENDPOINT=${shellQuote(gateway)} \\
  -e GRAM_MCP_SERVICE_ID=${shellQuote(serviceId)} \\
  -e GRAM_MCP_SERVICE_SLUG=${shellQuote(slug)} \\
  -e GRAM_MCP_SERVICE_VERSION=${shellQuote(serviceVersion)} \\
  ghcr.io/speakeasy-api/gram-tunnel-agent:latest`;
  const cli = `gram tunnel run \\
  --key ${shellQuote(renderedKey)} \\
  --upstream ${upstream} \\
  --service-id ${shellQuote(serviceId)} \\
  --service-slug ${shellQuote(slug)} \\
  --service-version ${shellQuote(serviceVersion)}`;

  return (
    <div className="rounded-lg border p-6">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div>
          <Type variant="subheading">Connect your MCP server</Type>
          <Type muted small className="mt-1">
            Start a tunnel agent next to the MCP server you already run.
          </Type>
        </div>
        {keyPrefix && (
          <Badge variant="neutral">
            <Badge.Text>{keyPrefix}</Badge.Text>
          </Badge>
        )}
      </div>

      <RequiredTunnelIdentity
        serviceId={serviceId}
        serviceSlug={slug}
        serviceVersion={serviceVersion}
        upstream={upstream}
      />

      <Tabs defaultValue="kubernetes">
        <TabsList>
          <TabsTrigger value="kubernetes">Kubernetes</TabsTrigger>
          <TabsTrigger value="docker">Docker</TabsTrigger>
          <TabsTrigger value="cli">CLI</TabsTrigger>
        </TabsList>
        <TabsContent value="kubernetes" className="mt-4">
          <div className="mb-3">
            <Type variant="subheading">Hello world MCP</Type>
            <Type muted small className="mt-1">
              Run a tiny Python MCP server and the tunnel agent in the same pod.
            </Type>
          </div>
          <CodeBlock language="yaml">{kubernetes}</CodeBlock>
        </TabsContent>
        <TabsContent value="docker" className="mt-4">
          <CodeBlock language="bash">{docker}</CodeBlock>
        </TabsContent>
        <TabsContent value="cli" className="mt-4">
          <CodeBlock language="bash">{cli}</CodeBlock>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function RequiredTunnelIdentity({
  serviceId,
  serviceSlug,
  serviceVersion,
  upstream,
}: {
  serviceId: string;
  serviceSlug: string;
  serviceVersion: string;
  upstream: string;
}) {
  const fields = [
    {
      key: "GRAM_MCP_SERVICE_ID",
      value: serviceId,
      description: "Stable ID for the MCP service behind this tunnel.",
    },
    {
      key: "GRAM_MCP_SERVICE_SLUG",
      value: serviceSlug,
      description: "Readable slug reported by each tunnel connection.",
    },
    {
      key: "GRAM_MCP_SERVICE_VERSION",
      value: serviceVersion,
      description: "Version of the MCP service whose manifest is uploaded.",
    },
    {
      key: "GRAM_TUNNEL_UPSTREAM",
      value: upstream,
      description: "Local Streamable HTTP endpoint the agent proxies to.",
    },
  ];

  return (
    <div className="bg-muted/30 mb-5 rounded-md border p-4">
      <Type variant="subheading" className="mb-1">
        Required tunnel identity
      </Type>
      <Type muted small className="mb-3 max-w-3xl">
        Each tunnel connection declares the same MCP service identity before it
        registers a manifest. Gram uses this to compare connected agents,
        surface service versions, and preserve session affinity for MCP
        consumers.
      </Type>
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        {fields.map((field) => (
          <div key={field.key} className="min-w-0">
            <Type small mono className="truncate">
              {field.key}
            </Type>
            <Type small className="mt-1 truncate font-medium">
              {field.value}
            </Type>
            <Type muted small className="mt-1">
              {field.description}
            </Type>
          </div>
        ))}
      </div>
    </div>
  );
}

function slugForSnippet(name: string | undefined): string {
  const slug = (name ?? "internal-mcp")
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
  return slug || "internal-mcp";
}

function yamlQuote(value: string): string {
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\\''")}'`;
}
