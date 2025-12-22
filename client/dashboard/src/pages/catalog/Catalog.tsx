import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
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
  CheckCircle,
  Loader2,
  Search,
  SearchXIcon,
  Server as ServerIcon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Outlet } from "react-router";

export function CatalogRoot() {
  return <Outlet />;
}

function generateSlug(name: string): string {
  // Extract the last part after "/" for reverse-DNS names like "ai.exa/exa"
  const lastPart = name.split("/").pop() || name;
  // Convert to lowercase, replace non-alphanumeric with dashes, collapse multiple dashes
  return lastPart
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export default function Catalog() {
  const routes = useRoutes();
  const client = useSdkClient();
  const [searchQuery, setSearchQuery] = useState("");
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteListMCPCatalog(searchQuery);
  const loadMoreRef = useRef<HTMLDivElement>(null);
  const [addingServer, setAddingServer] = useState<Server | null>(null);
  const [desiredToolsetName, setDesiredToolsetName] = useState("");
  const [createdToolsetSlug, setCreatedToolsetSlug] = useState<string | null>(
    null,
  );

  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const addServerMutation = useMutation({
    mutationFn: async ({
      server,
      toolsetName,
    }: {
      server: Server;
      toolsetName: string;
    }) => {
      const slug = generateSlug(toolsetName);
      let toolUrns = [`tools:externalmcp:${slug}:proxy`];
      if (server.tools && server.tools.length > 0) {
        toolUrns = server.tools.map(
          (tool) => `tools:externalmcp:${slug}:${tool.name}`,
        );
      }

      // 1. Evolve deployment with the external MCP source
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

      // 2. Create the toolset
      const toolset = await client.toolsets.create({
        createToolsetRequestBody: {
          name: toolsetName,
          description:
            server.description ?? `MCP server: ${server.registrySpecifier}`,
          toolUrns: toolUrns,
        },
      });

      return toolset.slug;
    },
    onSuccess: async (toolsetSlug) => {
      await refetchDeployment();
      setCreatedToolsetSlug(toolsetSlug);
    },
  });

  useEffect(() => {
    setDesiredToolsetName(addingServer?.title ?? "");
  }, [addingServer]);

  // Reset state when dialog closes
  useEffect(() => {
    if (!addingServer) {
      setCreatedToolsetSlug(null);
      addServerMutation.reset();
    }
  }, [addingServer]);

  // Flatten all pages into a single list
  const allServers = useMemo(() => {
    return data?.pages.flatMap((page) => page.servers as Server[]) ?? [];
  }, [data]);

  // Infinite scroll with IntersectionObserver
  useEffect(() => {
    const element = loadMoreRef.current;
    if (!element) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasNextPage && !isFetchingNextPage) {
          fetchNextPage();
        }
      },
      { threshold: 0.1 },
    );

    observer.observe(element);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage]);

  const addToProjectDialog = (
    <Dialog
      open={addingServer !== null}
      onOpenChange={(open) => setAddingServer(open ? addingServer : null)}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>
            {addingServer ? (
              <ServerHeading server={addingServer} />
            ) : (
              "Add MCP Server"
            )}
          </Dialog.Title>
          <Dialog.Description className="border-t pt-3">
            {createdToolsetSlug
              ? "MCP server imported successfully!"
              : "Import this MCP server into your project. This will create a new toolset and corresponding MCP server."}
          </Dialog.Description>
        </Dialog.Header>
        {createdToolsetSlug ? (
          <Stack direction="vertical" gap={4} className="py-4">
            <Stack direction="horizontal" gap={2} align="center">
              <CheckCircle className="w-5 h-5 text-green-500" />
              <Type>Toolset created successfully</Type>
            </Stack>
            <routes.toolsets.toolset.Link params={[createdToolsetSlug]}>
              <Button className="w-full">
                <Button.Text>View Toolset</Button.Text>
              </Button>
            </routes.toolsets.toolset.Link>
          </Stack>
        ) : (
          <>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                if (addingServer) {
                  addServerMutation.mutate({
                    server: addingServer,
                    toolsetName: desiredToolsetName,
                  });
                }
              }}
              className="flex flex-col gap-2 mt-2 mb-4"
            >
              <Label>Toolset Name</Label>
              <Input
                placeholder="Toolset name"
                value={desiredToolsetName}
                onChange={(e) => setDesiredToolsetName(e.target.value)}
                disabled={addServerMutation.isPending}
                required
              />
            </form>
            <Dialog.Footer>
              <Button
                variant="secondary"
                onClick={() => setAddingServer(null)}
                disabled={addServerMutation.isPending}
              >
                Cancel
              </Button>
              <Button
                disabled={
                  desiredToolsetName.length === 0 || addServerMutation.isPending
                }
                onClick={() => {
                  if (addingServer) {
                    addServerMutation.mutate({
                      server: addingServer,
                      toolsetName: desiredToolsetName,
                    });
                  }
                }}
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
          </>
        )}
      </Dialog.Content>
    </Dialog>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>MCP Catalog</Page.Section.Title>
          <Page.Section.Description>
            Import official MCP servers to your project. Powered by the official
            MCP registry.
          </Page.Section.Description>
          <Page.Section.Body>
            <Stack direction="vertical" gap={6}>
              <div className="relative">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                <Input
                  placeholder="Search MCP servers..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className="pl-10"
                />
              </div>

              <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                {isLoading &&
                  [...Array(6)].map((_, i) => (
                    <Skeleton key={i} className="h-[200px]" />
                  ))}
                {!isLoading &&
                  allServers?.map((server) => (
                    <MCPServerCard
                      key={`${server.registryId}-${server.registrySpecifier}`}
                      server={server}
                      onAddToProject={() => setAddingServer(server)}
                    />
                  ))}
              </div>

              {/* Load more trigger */}
              {!isLoading && hasNextPage && !searchQuery && (
                <div
                  ref={loadMoreRef}
                  className="flex justify-center items-center py-8"
                >
                  {isFetchingNextPage && (
                    <Stack direction="horizontal" gap={2} align="center">
                      <Loader2 className="w-4 h-4 animate-spin text-muted-foreground" />
                      <Type small muted>
                        Loading more...
                      </Type>
                    </Stack>
                  )}
                </div>
              )}

              {!isLoading && allServers?.length === 0 && (
                <EmptySearchResult onClear={() => setSearchQuery("")} />
              )}
            </Stack>
            {addToProjectDialog}
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function EmptySearchResult({ onClear }: { onClear: () => void }) {
  return (
    <div className="w-full  flex items-center justify-center bg-background rounded-xl border-1 py-8">
      <Stack
        gap={1}
        className="w-full max-w-sm m-8"
        align="center"
        justify="center"
      >
        <div className="py-6">
          <SearchXIcon className="size-16 text-foreground" />
        </div>
        <Heading variant="h5" className="font-medium">
          No matching entries
        </Heading>
        <Type small muted className="mb-4 text-center">
          No MCP servers match your query. Try adjusting or clearing your
          search.
        </Type>
        <Button onClick={onClear} size="sm">
          Clear Search
        </Button>
      </Stack>
    </div>
  );
}

interface MCPServerCardProps {
  server: Server;
  onAddToProject: (server: Server) => void;
}

function ServerHeading({ server }: { server: Server }) {
  const meta = server.meta["com.pulsemcp/server"];
  const isOfficial = meta?.isOfficial;

  const displayName = server.title ?? server.registrySpecifier;

  return (
    <Stack direction="horizontal" justify="space-between">
      <Stack direction="horizontal" gap={3}>
        <div className="w-12 h-12 rounded-lg bg-primary/10 flex items-center justify-center shrink-0">
          {server.iconUrl ? (
            <img
              src={server.iconUrl}
              alt={displayName}
              className="w-8 h-8 rounded"
            />
          ) : (
            <ServerIcon className="w-6 h-6 text-muted-foreground" />
          )}
        </div>
        <Stack gap={1}>
          <Stack direction="horizontal" gap={2} align="center">
            <Type variant="subheading">{displayName}</Type>
            {isOfficial && <Badge>Official</Badge>}
          </Stack>
          <Type small muted>
            {server.registrySpecifier} • v{server.version}
          </Type>
        </Stack>
      </Stack>
    </Stack>
  );
}

function MCPServerCard({ server, onAddToProject }: MCPServerCardProps) {
  const meta = server.meta["com.pulsemcp/server"];
  const visitorsTotal = meta?.visitorsEstimateLastFourWeeks;

  return (
    <Card>
      <Card.Title>
        <ServerHeading server={server} />
      </Card.Title>
      <Card.Description className="line-clamp-2 whitespace-pre-wrap">
        {server.description}
      </Card.Description>
      <Card.Footer>
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="w-full"
        >
          {visitorsTotal && visitorsTotal > 0 ? (
            <Type small muted>
              Usage: {visitorsTotal.toLocaleString()}
            </Type>
          ) : (
            <div />
          )}
          <Button
            variant="secondary"
            size="sm"
            onClick={() => onAddToProject(server)}
          >
            <Button.Text>Add to Project</Button.Text>
          </Button>
        </Stack>
      </Card.Footer>
    </Card>
  );
}
