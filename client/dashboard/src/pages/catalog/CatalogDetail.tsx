import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { Server, useInfiniteListMCPCatalog } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query";
import { Badge, Button, Input, Stack } from "@speakeasy-api/moonshine";
import { useMutation } from "@tanstack/react-query";
import {
  ArrowRight,
  CheckCircle,
  ChevronDown,
  ChevronUp,
  ExternalLink,
  Loader2,
  MessageCircle,
  Minus,
  Plus,
  Plug,
  Server as ServerIcon,
  Wrench,
} from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { Outlet, useParams } from "react-router";

// Map of server specifiers to their website URLs
const SERVER_WEBSITE_MAP: Record<string, string> = {
  "com.figma.mcp/mcp": "figma.com",
  "com.stripe/mcp": "stripe.com",
  "app.linear/linear": "linear.app",
  "io.github.getsentry/sentry-mcp": "sentry.io",
  "io.github.aws/mcp-proxy-for-aws": "aws.amazon.com",
  "io.github.grafana/mcp-grafana": "grafana.com",
};

export function CatalogDetailRoot() {
  return <Outlet />;
}

function generateSlug(name: string): string {
  const lastPart = name.split("/").pop() || name;
  return lastPart
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export default function CatalogDetail() {
  const { serverSpecifier } = useParams<{ serverSpecifier: string }>();
  const routes = useRoutes();
  const client = useSdkClient();
  const { data, isLoading } = useInfiniteListMCPCatalog();
  const [showAddDialog, setShowAddDialog] = useState(false);
  const [desiredToolsetName, setDesiredToolsetName] = useState("");
  const [createdToolsetSlug, setCreatedToolsetSlug] = useState<string | null>(null);

  const { data: deploymentResult, refetch: refetchDeployment } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const server = useMemo(() => {
    if (!data?.pages || !serverSpecifier) return null;
    const allServers = data.pages.flatMap((page) => page.servers as Server[]);
    // The specifier is URL encoded, so we need to decode it
    const decodedSpecifier = decodeURIComponent(serverSpecifier);
    return allServers.find((s) => s.registrySpecifier === decodedSpecifier) ?? null;
  }, [data, serverSpecifier]);

  const addServerMutation = useMutation({
    mutationFn: async ({
      server,
      toolsetName,
    }: {
      server: Server;
      toolsetName: string;
    }) => {
      const slug = generateSlug(server.registrySpecifier);
      const toolUrn = `tools:externalmcp:${slug}:proxy`;

      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          upsertExternalMcps: [
            {
              registryId: server.registryId,
              name: toolsetName,
              slug,
              registryServerSpecifier: server.registrySpecifier,
            },
          ],
        },
      });

      const toolset = await client.toolsets.create({
        createToolsetRequestBody: {
          name: toolsetName,
          description: server.description ?? `MCP server: ${server.registrySpecifier}`,
        },
      });

      await client.toolsets.updateBySlug({
        slug: toolset.slug,
        updateToolsetRequestBody: {
          toolUrns: [toolUrn],
          mcpEnabled: true,
          mcpIsPublic: true,
        },
      });

      return toolset.slug;
    },
    onSuccess: async (toolsetSlug) => {
      await refetchDeployment();
      setCreatedToolsetSlug(toolsetSlug);
    },
  });

  const removeServerMutation = useMutation({
    mutationFn: async (slug: string) => {
      const toolUrn = `tools:externalmcp:${slug}:proxy`;

      // Find and delete any toolsets that use this external MCP
      const toolsets = await client.toolsets.list();
      const matchingToolsets = toolsets.toolsets?.filter(
        (ts) => ts.toolUrns?.includes(toolUrn)
      ) ?? [];

      // Delete matching toolsets
      await Promise.all(
        matchingToolsets.map((ts) =>
          client.toolsets.deleteBySlug({ slug: ts.slug })
        )
      );

      // Remove the external MCP from the deployment
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          excludeExternalMcps: [slug],
        },
      });
    },
    onSuccess: async () => {
      await refetchDeployment();
    },
  });

  useEffect(() => {
    if (server) {
      setDesiredToolsetName(server.title ?? "");
    }
  }, [server]);

  useEffect(() => {
    if (!showAddDialog) {
      setCreatedToolsetSlug(null);
      addServerMutation.reset();
    }
  }, [showAddDialog]);

  const meta = server?.meta["com.pulsemcp/server"];
  const versionMeta = server?.meta["com.pulsemcp/server-version"];
  const isOfficial = meta?.isOfficial;
  const visitorsTotal = meta?.visitorsEstimateLastFourWeeks;
  const decodedSpecifier = serverSpecifier ? decodeURIComponent(serverSpecifier) : "";
  const displayName = server?.title ?? server?.registrySpecifier?.split("/").pop() ?? decodedSpecifier.split("/").pop();

  // Check if this server is already added to the project
  const existingExternalMcp = useMemo(() => {
    if (!deployment?.externalMcps || !server) return null;
    return deployment.externalMcps.find(
      (mcp) => mcp.registryServerSpecifier === server.registrySpecifier
    );
  }, [deployment?.externalMcps, server]);

  if (isLoading) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs
            substitutions={{ [encodeURIComponent(serverSpecifier || "")]: displayName }}
          />
        </Page.Header>
        <Page.Body>
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            <div className="lg:col-span-2">
              <Skeleton className="h-[400px] rounded-xl" />
            </div>
            <div>
              <Skeleton className="h-[200px] rounded-xl" />
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
            substitutions={{ [encodeURIComponent(serverSpecifier || "")]: displayName }}
          />
        </Page.Header>
        <Page.Body>
          <Card>
            <Card.Content className="py-12 text-center">
              <ServerIcon className="w-12 h-12 text-muted-foreground mx-auto mb-4" />
              <Type variant="subheading">Server not found</Type>
              <Type muted className="mt-2">
                The requested MCP server could not be found in the catalog.
              </Type>
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

  const addDialog = (
    <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
      <Dialog.Content className="gap-2">
        <Dialog.Header>
          <Dialog.Title>Add to Project</Dialog.Title>
          <Dialog.Description className="">
            {createdToolsetSlug
              ? ""
              : "Add this MCP server to your project. This will create a new toolset."}
          </Dialog.Description>
        </Dialog.Header>
        {createdToolsetSlug ? (
          <div className="pb-2">
            <Type small muted className="mb-3">
              <span className="font-medium text-foreground">{server.title || displayName}</span> has been added to your project. It will be available to deploy as an MCP server.
            </Type>
            <Type className="text-sm font-medium mb-2">Next steps</Type>
            <div className="flex flex-col gap-2">
              <routes.sources.Link className="no-underline hover:no-underline">
                <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline">
                  <div className="w-8 h-8 rounded-md bg-blue-500/10 dark:bg-blue-500/20 flex items-center justify-center shrink-0">
                    <Plus className="w-4 h-4 text-blue-600 dark:text-blue-400" />
                  </div>
                  <div className="flex-1">
                    <Type className="text-sm font-medium no-underline">Add more sources</Type>
                  </div>
                  <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                </div>
              </routes.sources.Link>
              <routes.elements.Link className="no-underline hover:no-underline">
                <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline">
                  <div className="w-8 h-8 rounded-md bg-violet-500/10 dark:bg-violet-500/20 flex items-center justify-center shrink-0">
                    <MessageCircle className="w-4 h-4 text-violet-600 dark:text-violet-400" />
                  </div>
                  <div className="flex-1">
                    <Type className="text-sm font-medium no-underline">Deploy as chat</Type>
                  </div>
                  <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                </div>
              </routes.elements.Link>
              <routes.mcp.details.hosted_page.Link params={[createdToolsetSlug]} className="no-underline hover:no-underline">
                <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline">
                  <div className="w-8 h-8 rounded-md bg-emerald-500/10 dark:bg-emerald-500/20 flex items-center justify-center shrink-0">
                    <Plug className="w-4 h-4 text-emerald-600 dark:text-emerald-400" />
                  </div>
                  <div className="flex-1">
                    <Type className="text-sm font-medium no-underline">Connect via Claude, Cursor</Type>
                  </div>
                  <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                </div>
              </routes.mcp.details.hosted_page.Link>
            </div>
          </div>
        ) : (
          <AddServerForm
            server={server}
            desiredToolsetName={desiredToolsetName}
            setDesiredToolsetName={setDesiredToolsetName}
            addServerMutation={addServerMutation}
            onCancel={() => setShowAddDialog(false)}
          />
        )}
      </Dialog.Content>
    </Dialog>
  );

  const weeklyUsage = meta?.visitorsEstimateMostRecentWeek;
  const totalUsage = meta?.visitorsEstimateTotal;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
            substitutions={{ [encodeURIComponent(serverSpecifier || "")]: displayName }}
          />
      </Page.Header>
      <Page.Body>
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
          {/* Left Column - Server Details */}
          <div className="lg:col-span-2 space-y-6">
            {/* Header */}
            <div className="flex items-start gap-6">
              <div className="w-24 h-24 rounded-xl bg-primary/5 flex items-center justify-center shrink-0">
                {server.iconUrl ? (
                  <img
                    src={server.iconUrl}
                    alt={displayName}
                    className="w-16 h-16 rounded-lg object-contain"
                  />
                ) : (
                  <ServerIcon className="w-12 h-12 text-muted-foreground" />
                )}
              </div>
              <div className="flex-1 min-w-0">
                <Stack direction="horizontal" gap={3} align="center" className="mb-2">
                  <h1 className="text-2xl font-bold">{displayName}</h1>
                  {isOfficial && <Badge>Official</Badge>}
                  {versionMeta?.isLatest && <Badge variant="secondary">Latest</Badge>}
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
                  <Type muted className="font-mono text-sm">
                    {server.registrySpecifier}
                  </Type>
                )}
                <div className="mt-4">
                  {existingExternalMcp ? (
                    <Button
                      variant="secondary"
                      size="md"
                      onClick={() => removeServerMutation.mutate(existingExternalMcp.slug)}
                      disabled={removeServerMutation.isPending}
                    >
                      {removeServerMutation.isPending ? (
                        <>
                          <Loader2 className="w-4 h-4 animate-spin" />
                          <Button.Text>Removing...</Button.Text>
                        </>
                      ) : (
                        <>
                          <Minus className="w-4 h-4" />
                          <Button.Text>Remove</Button.Text>
                        </>
                      )}
                    </Button>
                  ) : (
                    <Button size="md" onClick={() => setShowAddDialog(true)}>
                      <Plus className="w-4 h-4" />
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
                <Type className="whitespace-pre-wrap leading-relaxed">
                  {server.description || "No description available."}
                </Type>
              </Card.Content>
            </Card>

            {/* Available Tools */}
            {versionMeta?.["remotes[0]"]?.tools && versionMeta["remotes[0]"].tools.length > 0 && (
              <ToolsSection tools={versionMeta["remotes[0]"].tools} />
            )}
          </div>

          {/* Right Column - Info */}
          <div className="space-y-4">
            {/* Usage Stats */}
            {(weeklyUsage || visitorsTotal || totalUsage) && (
              <Card>
                <Card.Header>
                  <Card.Title>Usage</Card.Title>
                </Card.Header>
                <Card.Content>
                  <div className="space-y-3">
                    {weeklyUsage !== undefined && weeklyUsage > 0 && (
                      <div className="flex justify-between gap-4">
                        <Type small muted>
                          This Week
                        </Type>
                        <Type className="font-medium">
                          {weeklyUsage.toLocaleString()}
                        </Type>
                      </div>
                    )}
                    {visitorsTotal !== undefined && visitorsTotal > 0 && (
                      <div className="flex justify-between gap-4">
                        <Type small muted>
                          Monthly
                        </Type>
                        <Type className="font-medium">
                          {visitorsTotal.toLocaleString()}
                        </Type>
                      </div>
                    )}
                    {totalUsage !== undefined && totalUsage > 0 && (
                      <div className="flex justify-between gap-4">
                        <Type small muted>
                          All Time
                        </Type>
                        <Type className="font-medium">
                          {totalUsage.toLocaleString()}
                        </Type>
                      </div>
                    )}
                  </div>
                </Card.Content>
              </Card>
            )}

            {/* Version & Release Info */}
            <Card>
              <Card.Header>
                <Card.Title>Version & Release</Card.Title>
              </Card.Header>
              <Card.Content>
                <div className="space-y-3">
                  <div className="flex justify-between gap-4">
                    <Type small muted>
                      Version
                    </Type>
                    <Type className="font-mono">{server.version}</Type>
                  </div>
                  {versionMeta?.status && (
                    <div className="flex justify-between gap-4">
                      <Type small muted>
                        Status
                      </Type>
                      <Type className="capitalize">{versionMeta.status}</Type>
                    </div>
                  )}
                  {versionMeta?.publishedAt && (
                    <div className="flex justify-between gap-4">
                      <Type small muted>
                        Published
                      </Type>
                      <Type>
                        {new Date(versionMeta.publishedAt).toLocaleDateString()}
                      </Type>
                    </div>
                  )}
                  {versionMeta?.updatedAt && (
                    <div className="flex justify-between gap-4">
                      <Type small muted>
                        Last Updated
                      </Type>
                      <Type>
                        {new Date(versionMeta.updatedAt).toLocaleDateString()}
                      </Type>
                    </div>
                  )}
                  {versionMeta?.source && (
                    <div className="flex justify-between gap-4">
                      <Type small muted>
                        Source
                      </Type>
                      <a
                        href={versionMeta.source.startsWith("http") ? versionMeta.source : `https://${versionMeta.source}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="flex items-center gap-1 text-primary hover:underline"
                      >
                        <Type className="text-right truncate max-w-[150px]">{versionMeta.source}</Type>
                        <ExternalLink className="w-3 h-3 shrink-0" />
                      </a>
                    </div>
                  )}
                </div>
              </Card.Content>
            </Card>

            {/* Registry Info */}
            <Card>
              <Card.Header>
                <Card.Title>Registry</Card.Title>
              </Card.Header>
              <Card.Content>
                <div className="space-y-3">
                  <div className="flex justify-between gap-4">
                    <Type small muted>
                      Registry
                    </Type>
                    <Type className="text-right">{server.registryId}</Type>
                  </div>
                  <div className="flex justify-between items-center gap-4">
                    <Type small muted>
                      Specifier
                    </Type>
                    <Type className="font-mono text-xs text-right break-all">
                      {server.registrySpecifier}
                    </Type>
                  </div>
                </div>
              </Card.Content>
            </Card>
          </div>
        </div>
        {addDialog}
      </Page.Body>
    </Page>
  );
}

function AddServerForm({
  server,
  desiredToolsetName,
  setDesiredToolsetName,
  addServerMutation,
  onCancel,
}: {
  server: Server;
  desiredToolsetName: string;
  setDesiredToolsetName: (name: string) => void;
  addServerMutation: ReturnType<typeof useMutation<string, Error, { server: Server; toolsetName: string }>>;
  onCancel: () => void;
}) {
  const handleSubmit = () => {
    addServerMutation.mutate({
      server,
      toolsetName: desiredToolsetName || server.title || server.registrySpecifier,
    });
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !addServerMutation.isPending) {
      e.preventDefault();
      handleSubmit();
    }
  };

  return (
    <div onKeyDown={handleKeyDown}>
      <div className="flex flex-col gap-2 py-2">
        <Label>Source name</Label>
        <Input
          placeholder={server.title || server.registrySpecifier}
          value={desiredToolsetName}
          onChange={(e) => setDesiredToolsetName(e.target.value)}
          disabled={addServerMutation.isPending}
        />
      </div>
      <Dialog.Footer>
        <Button
          variant="secondary"
          onClick={onCancel}
          disabled={addServerMutation.isPending}
        >
          Cancel
        </Button>
        <Button
          disabled={addServerMutation.isPending}
          onClick={handleSubmit}
        >
          {addServerMutation.isPending ? (
            <>
              <Loader2 className="w-4 h-4 animate-spin mr-2" />
              <Button.Text>Adding...</Button.Text>
            </>
          ) : (
            <Button.Text>Add</Button.Text>
          )}
        </Button>
      </Dialog.Footer>
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
  };
};

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
  const firstSentence = tool.description ? getFirstSentence(tool.description) : "";
  const hasMoreContent = tool.description && tool.description.length > firstSentence.length;

  return (
    <div className="flex flex-col gap-1 p-3 rounded-lg bg-muted/50 overflow-hidden">
      <button
        onClick={() => hasMoreContent && setIsExpanded(!isExpanded)}
        className={`flex flex-col gap-1 text-left w-full ${hasMoreContent ? "cursor-pointer" : "cursor-default"}`}
      >
        <Stack direction="horizontal" gap={2} align="center" justify="space-between" className="w-full">
          <Stack direction="horizontal" gap={2} align="center">
            <Type className="font-mono text-sm font-medium">{tool.name}</Type>
            {tool.annotations?.readOnlyHint && (
              <Badge variant="secondary" className="text-xs">Read-only</Badge>
            )}
          </Stack>
          {hasMoreContent && (
            <motion.div
              animate={{ rotate: isExpanded ? 180 : 0 }}
              transition={{ duration: 0.2 }}
            >
              <ChevronDown className="w-4 h-4 text-muted-foreground" />
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
              <Type small muted>
                {firstSentence}
              </Type>
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
            <div className="mt-2 pt-2 border-t">
              <Type small className="whitespace-pre-wrap prose prose-sm max-w-none">
                {tool.description}
              </Type>
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
            <Wrench className="w-4 h-4" />
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
            className="mt-4 w-full flex items-center justify-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors"
          >
            {showAll ? (
              <>
                Show less <ChevronUp className="w-4 h-4" />
              </>
            ) : (
              <>
                Show {tools.length - INITIAL_TOOLS_SHOWN} more tools <ChevronDown className="w-4 h-4" />
              </>
            )}
          </button>
        )}
      </Card.Content>
    </Card>
  );
}
