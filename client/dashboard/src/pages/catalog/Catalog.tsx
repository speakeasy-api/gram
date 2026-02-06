import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { AddServersDialog } from "@/pages/catalog/AddServerDialog";
import { CommandBar } from "@/pages/catalog/CommandBar";
import { Server, useInfiniteListMCPCatalog } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { useLatestDeployment } from "@gram/client/react-query";
import { Badge, Button, Input, Stack } from "@speakeasy-api/moonshine";
import {
  Loader2,
  Search,
  SearchXIcon,
  Server as ServerIcon,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link, Outlet, useNavigate } from "react-router";

export function CatalogRoot() {
  return <Outlet />;
}

export default function Catalog() {
  const routes = useRoutes();
  const client = useSdkClient();
  const organization = useOrganization();
  const project = useProject();
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedServers, setSelectedServers] = useState<Set<string>>(
    new Set(),
  );
  const [addingServers, setAddingServers] = useState<Server[]>([]);
  const [targetProjectSlug, setTargetProjectSlug] = useState(project.slug);
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteListMCPCatalog(searchQuery);
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;
  const externalMcps = deployment?.externalMcps ?? [];
  const loadMoreRef = useRef<HTMLDivElement>(null);

  // Flatten all pages into a single list
  const allServers = useMemo(() => {
    return data?.pages.flatMap((page) => page.servers as Server[]) ?? [];
  }, [data]);

  const toggleServerSelection = (serverKey: string) => {
    setSelectedServers((prev) => {
      const next = new Set(prev);
      if (next.has(serverKey)) {
        next.delete(serverKey);
      } else {
        next.add(serverKey);
      }
      return next;
    });
  };

  const clearSelection = () => setSelectedServers(new Set());

  const getSelectedServerObjects = () =>
    allServers.filter((s) =>
      selectedServers.has(`${s.registryId}-${s.registrySpecifier}`),
    );

  const projectOptions = organization.projects.map((p) => ({
    value: p.slug,
    label: p.name || p.slug,
  }));

  const handleAdd = () => {
    setAddingServers(getSelectedServerObjects());
  };

  const navigate = useNavigate();
  const handleGoToMCPs = () => {
    navigate(`/${organization.slug}/${targetProjectSlug}/mcp`);
  };

  const handleCreateProject = async (name: string) => {
    const result = await client.projects.create({
      createProjectRequestBody: {
        name,
        organizationId: organization.id,
      },
    });
    await organization.refetch();
    setTargetProjectSlug(result.project.slug);
  };

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
                  allServers?.map((server) => {
                    const serverKey = `${server.registryId}-${server.registrySpecifier}`;
                    return (
                      <MCPServerCard
                        key={serverKey}
                        server={server}
                        detailHref={routes.catalog.detail.href(
                          encodeURIComponent(server.registrySpecifier),
                        )}
                        isAdded={externalMcps.some(
                          (mcp) =>
                            mcp.registryServerSpecifier ===
                            server.registrySpecifier,
                        )}
                        isSelected={selectedServers.has(serverKey)}
                        onToggleSelect={() => toggleServerSelection(serverKey)}
                      />
                    );
                  })}
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
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
      <AddServersDialog
        servers={addingServers}
        projects={projectOptions}
        projectSlug={targetProjectSlug}
        onProjectChange={setTargetProjectSlug}
        onCreateProject={handleCreateProject}
        onGoToMCPs={handleGoToMCPs}
        open={addingServers.length > 0}
        onOpenChange={(open) => {
          if (!open) {
            setAddingServers([]);
            clearSelection();
          }
        }}
        onServersAdded={() => refetchDeployment()}
      />
      <CommandBar
        selectedCount={selectedServers.size}
        projects={projectOptions}
        currentProjectSlug={targetProjectSlug}
        onSelectProject={setTargetProjectSlug}
        onCreateProject={handleCreateProject}
        onAdd={handleAdd}
        onClear={clearSelection}
      />
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
  detailHref: string;
  isAdded: boolean;
  isSelected: boolean;
  onToggleSelect: () => void;
}

function MCPServerCard({
  server,
  detailHref,
  isAdded,
  isSelected,
  onToggleSelect,
}: MCPServerCardProps) {
  const meta = server.meta["com.pulsemcp/server"];
  const isOfficial = meta?.isOfficial;
  const visitorsTotal = meta?.visitorsEstimateLastFourWeeks;
  const displayName = server.title ?? server.registrySpecifier;

  return (
    <div
      className={`group relative flex flex-col gap-4 rounded-xl border bg-card p-5 hover:border-primary/50 hover:shadow-md transition-all cursor-pointer h-full ${
        isSelected ? "ring-2 ring-primary border-primary" : ""
      }`}
      onClick={onToggleSelect}
    >
      <Stack direction="horizontal" gap={3}>
        <div className="w-12 h-12 rounded-lg bg-primary/10 flex items-center justify-center shrink-0 group-hover:bg-primary/15 transition-colors">
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
        <Stack gap={1} className="min-w-0">
          <Stack direction="horizontal" gap={2} align="center">
            <Type
              variant="subheading"
              className="group-hover:text-primary transition-colors"
            >
              {displayName}
            </Type>
            {isOfficial && <Badge>Official</Badge>}
            {isAdded && <Badge>Added</Badge>}
          </Stack>
          <Type small muted>
            {server.registrySpecifier} • v{server.version}
          </Type>
        </Stack>
      </Stack>
      <Type small muted className="line-clamp-2">
        {server.description}
      </Type>
      <div className="mt-auto pt-2">
        <Stack direction="horizontal" justify="space-between" align="center">
          {visitorsTotal && visitorsTotal > 0 ? (
            <Type small muted>
              {visitorsTotal.toLocaleString()} monthly users
            </Type>
          ) : (
            <div />
          )}
          <Link to={detailHref} onClick={(e) => e.stopPropagation()}>
            <Button variant="secondary" size="sm">
              <Button.Text>View Details</Button.Text>
            </Button>
          </Link>
        </Stack>
      </div>
    </div>
  );
}
