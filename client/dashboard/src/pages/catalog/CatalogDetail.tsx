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
  PulseMCPServer,
  useIsCatalogServerInstalled,
  useListMCPCatalog,
} from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMcpRegistriesGetServerDetails } from "@gram/client/react-query/mcpRegistriesGetServerDetails.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Stack } from "@/components/ui/Stack";
import { useMutation } from "@tanstack/react-query";
import {
  ChevronDown,
  ChevronUp,
  ExternalLink,
  Loader2,
  Minus,
  Plus,
  Server as ServerIcon,
  Wrench,
} from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
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

// The endpoint a setup guide is looked up by. Gram installs the streamable-HTTP
// remote, so that is the URL a guide is most likely keyed on, but guides are
// published per server rather than per transport: an entry that only lists an
// SSE endpoint still has one, and would find it under no other key when the
// guide publishes no registry alias.
function setupGuideLookupUrl(server: PulseMCPServer): string | undefined {
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
    const allServers = data.servers as PulseMCPServer[];
    return (
      allServers.find((s) => s.registrySpecifier === decodedSpecifier) ?? null
    );
  }, [data, decodedSpecifier]);

  // The catalog list omits per-tool definitions to stay small, so fetch the
  // full tool list for the detail view separately (cached server-side).
  const { data: serverDetails } = useMcpRegistriesGetServerDetails(
    {
      registryId: server?.registryId ?? "",
      serverSpecifier: decodedSpecifier,
    },
    undefined,
    { enabled: !!server?.registryId && !!decodedSpecifier },
  );
  const detailTools: Tool[] = useMemo(
    () =>
      (serverDetails?.tools ?? [])
        .filter((tool) => !!tool.name)
        .map((tool) => ({
          name: tool.name as string,
          description: tool.description ?? undefined,
          annotations: tool.annotations as Tool["annotations"],
        })),
    [serverDetails],
  );

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

  const meta = server?.meta?.["com.pulsemcp/server"];
  const versionMeta = server?.meta?.["com.pulsemcp/server-version"];
  const isOfficial = meta?.isOfficial;
  const visitorsTotal = meta?.visitorsEstimateLastFourWeeks;
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
              <routes.catalog.Link className="mt-4 inline-block">
                <Button variant="secondary" className="mt-4">
                  <Button.Text>Back to Catalog</Button.Text>
                </Button>
              </routes.catalog.Link>
            </Card.Content>
          </Card>
        </Page.Body>
      </Page>
    );
  }

  const weeklyUsage = meta?.visitorsEstimateMostRecentWeek;
  const totalUsage = meta?.visitorsEstimateTotal;

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
          serverUrl={setupGuideLookupUrl(server)}
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
                <div className="bg-primary/5 flex h-24 w-24 shrink-0 items-center justify-center dark:bg-neutral-800">
                  {server.iconUrl ? (
                    <img
                      src={server.iconUrl}
                      alt={displayName}
                      className="h-16 w-16 object-contain"
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
                    {isOfficial && <Badge>Official</Badge>}
                    {versionMeta?.isLatest && (
                      <Badge variant="neutral">Latest</Badge>
                    )}
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

              {/* Available Tools */}
              {detailTools.length > 0 && <ToolsSection tools={detailTools} />}
            </div>

            {/* Right Column - Info */}
            <div>
              <Card>
                <Card.Content>
                  <div className="divide-y">
                    {(weeklyUsage || visitorsTotal || totalUsage) && (
                      <DetailGroup label="Usage">
                        {weeklyUsage !== undefined && weeklyUsage > 0 && (
                          <DetailRow label="This Week">
                            <Text className="font-medium">
                              {weeklyUsage.toLocaleString()}
                            </Text>
                          </DetailRow>
                        )}
                        {visitorsTotal !== undefined && visitorsTotal > 0 && (
                          <DetailRow label="Monthly">
                            <Text className="font-medium">
                              {visitorsTotal.toLocaleString()}
                            </Text>
                          </DetailRow>
                        )}
                        {totalUsage !== undefined && totalUsage > 0 && (
                          <DetailRow label="All Time">
                            <Text className="font-medium">
                              {totalUsage.toLocaleString()}
                            </Text>
                          </DetailRow>
                        )}
                      </DetailGroup>
                    )}

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
                            {new Date(
                              versionMeta.publishedAt,
                            ).toLocaleDateString()}
                          </Text>
                        </DetailRow>
                      )}
                      {versionMeta?.updatedAt && (
                        <DetailRow label="Last Updated">
                          <Text>
                            {new Date(
                              versionMeta.updatedAt,
                            ).toLocaleDateString()}
                          </Text>
                        </DetailRow>
                      )}
                      {versionMeta?.source && (
                        <DetailRow label="Source">
                          <a
                            href={
                              versionMeta.source.startsWith("http")
                                ? versionMeta.source
                                : `https://${versionMeta.source}`
                            }
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-primary flex items-center gap-1 hover:underline"
                          >
                            <Text className="max-w-[150px] truncate text-right">
                              {versionMeta.source}
                            </Text>
                            <ExternalLink className="h-3 w-3 shrink-0" />
                          </a>
                        </DetailRow>
                      )}
                    </DetailGroup>

                    <DetailGroup label="Registry">
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

const INITIAL_TOOLS_SHOWN = 5;

type Tool = {
  name: string;
  description?: string;
  annotations?: {
    title?: string;
    readOnlyHint?: boolean;
    destructiveHint?: boolean;
    idempotentHint?: boolean;
    openWorldHint?: boolean;
  };
};

// Registry descriptions are often markdown; strip the common inline syntax
// (heading #s, bold markers, backticks) so the collapsed one-line summary
// reads as plain text.
function stripMarkdown(text: string): string {
  return text
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/`([^`]+)`/g, "$1")
    .trim();
}

function getFirstSentence(text: string): string {
  // Find the first period followed by a space or end of string
  const match = text.match(/^[^.]*\./);
  if (match) {
    return match[0];
  }
  // If no period, return first 100 chars
  return text.length > 100 ? text.slice(0, 100) + "..." : text;
}

function ToolCard({ tool }: { tool: Tool }) {
  const [isExpanded, setIsExpanded] = useState(false);
  const hasDescription = !!tool.description;
  const plainDescription = tool.description
    ? stripMarkdown(tool.description)
    : "";
  const firstSentence = plainDescription
    ? getFirstSentence(plainDescription)
    : "";
  const hasMoreContent =
    !!plainDescription && plainDescription.length > firstSentence.length;

  return (
    <div className="bg-muted/50 flex flex-col gap-1 overflow-hidden p-3">
      <button
        onClick={() => {
          void (hasMoreContent && setIsExpanded(!isExpanded));
        }}
        className={`flex w-full flex-col gap-1 text-left ${hasMoreContent ? "cursor-pointer" : "cursor-default"}`}
      >
        <Stack
          direction="horizontal"
          gap={2}
          align="center"
          justify="space-between"
          className="w-full"
        >
          <Stack
            direction="horizontal"
            gap={2}
            align="center"
            className="flex-wrap"
          >
            <Text className="font-mono text-sm font-medium">
              {tool.annotations?.title || tool.name}
            </Text>
            {tool.annotations?.readOnlyHint && (
              <Badge variant="neutral" background className="text-xs">
                Read-only
              </Badge>
            )}
            {tool.annotations?.destructiveHint &&
              !tool.annotations?.readOnlyHint && (
                <Badge variant="warning" className="text-xs">
                  Destructive
                </Badge>
              )}
            {tool.annotations?.idempotentHint &&
              !tool.annotations?.readOnlyHint && (
                <Badge variant="information" className="text-xs">
                  Idempotent
                </Badge>
              )}
            {!tool.annotations?.readOnlyHint &&
              !tool.annotations?.destructiveHint &&
              !tool.annotations?.idempotentHint && (
                <Badge variant="information" background className="text-xs">
                  Write
                </Badge>
              )}
          </Stack>
          {hasMoreContent && (
            <motion.div
              animate={{ rotate: isExpanded ? 180 : 0 }}
              transition={{ duration: 0.2 }}
            >
              <ChevronDown className="text-muted-foreground h-4 w-4" />
            </motion.div>
          )}
        </Stack>
        <AnimatePresence mode="wait">
          {hasDescription && !isExpanded && (
            <motion.div
              key="collapsed"
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0, height: 0 }}
              transition={{ duration: 0.2 }}
            >
              <Text small muted>
                {firstSentence}
              </Text>
            </motion.div>
          )}
        </AnimatePresence>
      </button>
      <AnimatePresence>
        {isExpanded && tool.description && (
          <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: "auto" }}
            exit={{ opacity: 0, height: 0 }}
            transition={{ duration: 0.2 }}
            className="overflow-hidden"
          >
            <div className="mt-2 border-t pt-2">
              <Text
                small
                className="prose prose-sm max-w-none whitespace-pre-wrap"
              >
                {tool.description}
              </Text>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function ToolsSection({ tools }: { tools: Tool[] }) {
  const [showAll, setShowAll] = useState(false);
  const hasMore = tools.length > INITIAL_TOOLS_SHOWN;
  const visibleTools = showAll ? tools : tools.slice(0, INITIAL_TOOLS_SHOWN);

  return (
    <Card>
      <Card.Header>
        <Card.Title>
          <Stack direction="horizontal" gap={2} align="center">
            <Wrench className="h-4 w-4" />
            Available Tools ({tools.length})
          </Stack>
        </Card.Title>
      </Card.Header>
      <Card.Content>
        <div className="space-y-3">
          {visibleTools.map((tool) => (
            <ToolCard key={tool.name} tool={tool} />
          ))}
        </div>
        {hasMore && (
          <button
            onClick={() => setShowAll(!showAll)}
            className="text-muted-foreground hover:text-foreground mt-4 flex w-full items-center justify-center gap-1 text-sm transition-colors"
          >
            {showAll ? (
              <>
                Show less <ChevronUp className="h-4 w-4" />
              </>
            ) : (
              <>
                Show {tools.length - INITIAL_TOOLS_SHOWN} more tools{" "}
                <ChevronDown className="h-4 w-4" />
              </>
            )}
          </button>
        )}
      </Card.Content>
    </Card>
  );
}
