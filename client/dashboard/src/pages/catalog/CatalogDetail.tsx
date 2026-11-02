import { catalogLogoClassName } from "./logo";
import { cn } from "@/lib/utils";
import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/Card";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { SetupGuideCallout } from "@/components/setup-guide/SetupGuideCallout";
import { ManualSetupBadge } from "@/pages/catalog/ManualSetupBadge";
import { useSdkClient } from "@/contexts/Sdk";
import { filterToHttpRemotes } from "@/pages/catalog/remotes";
import { AddServerDialog } from "@/pages/catalog/AddServerDialog";
import {
  CatalogServer,
  useIsCatalogServerInstalled,
  useListMCPCatalog,
} from "@/pages/catalog/hooks";
import { parseServerMetadata } from "@/pages/catalog/hooks/serverMetadata";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Stack } from "@/components/ui/Stack";
import { useMutation } from "@tanstack/react-query";
import { Loader2, Minus, Plus, Server as ServerIcon } from "lucide-react";
import { ReactNode, useMemo, useState } from "react";
import { Outlet, useParams } from "react-router";

// Map of server specifiers to their website URLs
const SERVER_WEBSITE_MAP: Record<string, string> = {
  "com.figma.mcp/mcp": "figma.com",
  "com.stripe/mcp": "stripe.com",
  "app.linear/linear": "linear.app",
  "io.github.getsentry/sentry-mcp": "sentry.io",
  "io.github.github/github-mcp-server": "github.com",
  "com.notion/mcp": "notion.so",
};

export function CatalogDetailRoot(): JSX.Element {
  return <Outlet />;
}

// The server's own endpoint: the one shown as its Server URL and the one a
// setup guide is looked up by. Speakeasy installs the streamable-HTTP remote, so
// that comes first, but guides are published per server rather than per
// transport: an entry that only lists an SSE endpoint still has one, and would
// find it under no other key when the guide publishes no registry alias.
function primaryRemoteUrl(server: CatalogServer): string | undefined {
  return (
    filterToHttpRemotes(server).remotes?.[0]?.url ?? server.remotes?.[0]?.url
  );
}

export default function CatalogDetail(): JSX.Element {
  const { serverSpecifier } = useParams<{ serverSpecifier: string }>();
  const routes = useRoutes();
  const client = useSdkClient();
  const decodedSpecifier = serverSpecifier
    ? decodeURIComponent(serverSpecifier)
    : "";
  // The catalog is small and fetched in full, so reuse the unparameterized
  // catalog query (shared cache key with the list page) and find the server
  // client-side.
  const { data, isLoading } = useListMCPCatalog();
  const [showAddDialog, setShowAddDialog] = useState(false);

  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const { data: toolsetsResult } = useListToolsets();

  const server = useMemo(() => {
    if (!data?.servers || !decodedSpecifier) return null;
    const allServers = data.servers as CatalogServer[];
    return (
      allServers.find((s) => s.registrySpecifier === decodedSpecifier) ?? null
    );
  }, [data, decodedSpecifier]);

  const removeServerMutation = useMutation({
    mutationFn: async (slug: string) => {
      const toolUrn = `tools:externalmcp:${slug}:proxy`;

      // Find and delete any toolsets that use this external MCP
      const toolsets = await client.toolsets.list();
      const matchingToolsets =
        toolsets.toolsets?.filter((ts) => ts.toolUrns?.includes(toolUrn)) ?? [];

      // Delete matching toolsets
      await Promise.all(
        matchingToolsets.map((ts) =>
          client.toolsets.deleteBySlug({ slug: ts.slug }),
        ),
      );

      // Remove the external MCP from the deployment
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          nonBlocking: true,
          excludeExternalMcps: [slug],
        },
      });
    },
    onSuccess: async () => {
      await refetchDeployment();
    },
  });

  const versionMeta = server ? parseServerMetadata(server) : undefined;
  const serverUrl = server ? primaryRemoteUrl(server) : undefined;
  const displayName =
    server?.title ??
    server?.registrySpecifier?.split("/").pop() ??
    decodedSpecifier.split("/").pop();

  // Check if this server is already added to the project
  const existingExternalMcp = useMemo(() => {
    if (!deployment?.externalMcps || !server) return null;
    return deployment.externalMcps.find(
      (mcp) => mcp.registryServerSpecifier === server.registrySpecifier,
    );
  }, [deployment?.externalMcps, server]);

  // Also consider origin-backed toolsets as installed — legacy installs
  // created toolsets stamped with the catalog origin.
  const hasOriginMatch = useMemo(() => {
    if (!server) return false;
    return (toolsetsResult?.toolsets ?? []).some(
      (toolset) =>
        toolset.origin?.registrySpecifier === server.registrySpecifier,
    );
  }, [toolsetsResult?.toolsets, server]);

  // New installs are remote MCP servers, matched back to the catalog entry by
  // endpoint URL.
  const isInstalledByUrl = useIsCatalogServerInstalled();

  const isInstalled =
    !!existingExternalMcp ||
    hasOriginMatch ||
    (!!server && isInstalledByUrl(server));

  if (isLoading) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs
            substitutions={{
              [encodeURIComponent(serverSpecifier || "")]: displayName,
            }}
          />
        </Page.Header>
        <Page.Body>
          <div className="@container">
            <div className="grid grid-cols-1 gap-8 @3xl:grid-cols-3">
              <div className="@3xl:col-span-2">
                <Skeleton className="h-[400px]" />
              </div>
              <div>
                <Skeleton className="h-[200px]" />
              </div>
            </div>
          </div>
        </Page.Body>
      </Page>
    );
  }

  if (!server) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs
            substitutions={{
              [encodeURIComponent(serverSpecifier || "")]: displayName,
            }}
          />
        </Page.Header>
        <Page.Body>
          <Card>
            <Card.Content className="py-12 text-center">
              <ServerIcon className="text-muted-foreground mx-auto mb-4 h-12 w-12" />
              <Text variant="subheading">Server not found</Text>
              <Text muted className="mt-2">
                The requested MCP server could not be found in the catalog.
              </Text>
              <routes.mcp.catalog.Link className="mt-4 inline-block">
                <Button variant="secondary" className="mt-4">
                  <Button.Text>Back to Catalog</Button.Text>
                </Button>
              </routes.mcp.catalog.Link>
            </Card.Content>
          </Card>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [encodeURIComponent(serverSpecifier || "")]: displayName,
          }}
        />
      </Page.Header>
      <Page.Body>
        <SetupGuideCallout
          registrySpecifier={server.registrySpecifier}
          serverUrl={serverUrl}
          iconUrl={server.iconUrl}
        />
        {/* Container query, not a viewport one: the side panel narrows this
            column without narrowing the window, and `lg:` would not notice. */}
        <div className="@container">
          <div className="grid grid-cols-1 items-start gap-8 @3xl:grid-cols-3">
            {/* Left Column - Server Details */}
            <div className="space-y-6 @3xl:col-span-2">
              {/* Header */}
              <div className="flex items-start gap-6">
                <div className="bg-card flex h-24 w-24 shrink-0 items-center justify-center border">
                  {server.iconUrl ? (
                    <img
                      src={server.iconUrl}
                      alt={displayName}
                      className={cn(
                        "h-16 w-16 object-contain",
                        catalogLogoClassName(server.registrySpecifier),
                      )}
                    />
                  ) : (
                    <ServerIcon className="text-muted-foreground h-12 w-12" />
                  )}
                </div>
                <div className="min-w-0 flex-1">
                  <Page.Eyebrow className="mb-2" />
                  <Stack
                    direction="horizontal"
                    gap={3}
                    align="center"
                    className="mb-2"
                  >
                    <h1 className="text-display-sm font-thin">{displayName}</h1>
                    <ManualSetupBadge server={server} />
                  </Stack>
                  {SERVER_WEBSITE_MAP[server.registrySpecifier] ? (
                    <a
                      href={`https://${SERVER_WEBSITE_MAP[server.registrySpecifier]}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-sm text-sky-500 hover:text-sky-600 hover:underline"
                    >
                      {SERVER_WEBSITE_MAP[server.registrySpecifier]}
                    </a>
                  ) : (
                    <Text muted className="font-mono text-sm">
                      {server.registrySpecifier}
                    </Text>
                  )}
                  <div className="mt-4">
                    {isInstalled ? (
                      <Stack direction="horizontal" gap={2} align="center">
                        {existingExternalMcp && (
                          <Button
                            variant="secondary"
                            size="md"
                            onClick={() =>
                              removeServerMutation.mutate(
                                existingExternalMcp.slug,
                              )
                            }
                            disabled={removeServerMutation.isPending}
                          >
                            <Button.LeftIcon>
                              {removeServerMutation.isPending ? (
                                <Loader2 className="h-4 w-4 animate-spin" />
                              ) : (
                                <Minus className="h-4 w-4" />
                              )}
                            </Button.LeftIcon>
                            <Button.Text>
                              {removeServerMutation.isPending
                                ? "Removing..."
                                : "Remove"}
                            </Button.Text>
                          </Button>
                        )}
                        <Button
                          size="md"
                          onClick={() => setShowAddDialog(true)}
                        >
                          <Button.LeftIcon>
                            <Plus className="h-4 w-4" />
                          </Button.LeftIcon>
                          <Button.Text>Add another</Button.Text>
                        </Button>
                      </Stack>
                    ) : (
                      <Button size="md" onClick={() => setShowAddDialog(true)}>
                        <Button.LeftIcon>
                          <Plus className="h-4 w-4" />
                        </Button.LeftIcon>
                        <Button.Text>Add</Button.Text>
                      </Button>
                    )}
                  </div>
                </div>
              </div>

              {/* About */}
              <Card>
                <Card.Header>
                  <Card.Title>About</Card.Title>
                </Card.Header>
                <Card.Content>
                  <Text className="leading-relaxed whitespace-pre-wrap">
                    {server.description || "No description available."}
                  </Text>
                </Card.Content>
              </Card>
            </div>

            {/* Right Column - Info */}
            <div>
              <Card>
                <Card.Content>
                  <div className="divide-y">
                    <DetailGroup label="Version & Release">
                      <DetailRow label="Version">
                        <Text className="font-mono">{server.version}</Text>
                      </DetailRow>
                      {versionMeta?.status && (
                        <DetailRow label="Status">
                          <Text className="capitalize">
                            {versionMeta.status}
                          </Text>
                        </DetailRow>
                      )}
                      {versionMeta?.publishedAt && (
                        <DetailRow label="Published">
                          <Text>
                            {versionMeta.publishedAt.toLocaleDateString()}
                          </Text>
                        </DetailRow>
                      )}
                      {versionMeta?.updatedAt && (
                        <DetailRow label="Last Updated">
                          <Text>
                            {versionMeta.updatedAt.toLocaleDateString()}
                          </Text>
                        </DetailRow>
                      )}
                    </DetailGroup>

                    <DetailGroup label="Registry">
                      {serverUrl ? (
                        <DetailRow label="Server URL">
                          <div className="flex min-w-0 items-center gap-1">
                            <Text
                              className="truncate font-mono text-xs"
                              title={serverUrl}
                            >
                              {serverUrl.replace(/^https?:\/\//, "")}
                            </Text>
                            <CopyButton
                              text={serverUrl}
                              size="xs"
                              tooltip="Copy server URL"
                            />
                          </div>
                        </DetailRow>
                      ) : null}
                      <DetailRow label="Registry">
                        <div className="flex min-w-0 items-center gap-1">
                          <Text className="truncate font-mono text-xs">
                            {server.registryId}
                          </Text>
                          {server.registryId && (
                            <CopyButton
                              text={server.registryId}
                              size="xs"
                              tooltip="Copy registry ID"
                            />
                          )}
                        </div>
                      </DetailRow>
                      <DetailRow label="Specifier">
                        <Text className="text-right font-mono text-xs break-all">
                          {server.registrySpecifier}
                        </Text>
                      </DetailRow>
                    </DetailGroup>
                  </div>
                </Card.Content>
              </Card>
            </div>
          </div>
        </div>
        <AddServerDialog
          servers={[server]}
          open={showAddDialog}
          onOpenChange={setShowAddDialog}
          onServersAdded={() => {
            void refetchDeployment();
          }}
        />
      </Page.Body>
    </Page>
  );
}

function DetailGroup({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="space-y-3 py-4 first:pt-0 last:pb-0">
      <div className="text-eyebrow">{label}</div>
      {children}
    </div>
  );
}

function DetailRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-4">
      <Text small muted>
        {label}
      </Text>
      {children}
    </div>
  );
}
