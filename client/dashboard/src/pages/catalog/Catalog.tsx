import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useSampleListRegistry } from "@/pages/catalog/useSampleListRegistry";
import { Badge, Button, Input, Stack } from "@speakeasy-api/moonshine";
import { Loader2, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Outlet } from "react-router";

export function CatalogRoot() {
  return <Outlet />;
}

export default function Catalog() {
  const [searchQuery, setSearchQuery] = useState("");
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useSampleListRegistry();
  const loadMoreRef = useRef<HTMLDivElement>(null);

  // Flatten all pages into a single list
  const allServers = useMemo(() => {
    return data?.pages.flatMap((page) => page.servers) ?? [];
  }, [data]);

  const filteredServers = useMemo(() => {
    const query = searchQuery.toLowerCase();
    return allServers.filter(
      (server) =>
        server.title.toLowerCase().includes(query) ||
        server.name.toLowerCase().includes(query) ||
        server.description.toLowerCase().includes(query),
    );
  }, [allServers, searchQuery]);

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
                  filteredServers?.map((server) => (
                    <MCPServerCard key={server.name} server={server} />
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

              {!isLoading && filteredServers?.length === 0 && (
                <div className="text-center py-12">
                  <Type variant="subheading" className="text-muted-foreground">
                    No MCP servers found matching "{searchQuery}"
                  </Type>
                </div>
              )}
            </Stack>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

interface MCPServerCardProps {
  server: {
    name: string;
    version: string;
    description: string;
    title: string;
    logo: string;
    meta: {
      "com.pulsemcp/server"?: {
        visitorsEstimateMostRecentWeek?: number;
        visitorsEstimateLastFourWeeks?: number;
        visitorsEstimateTotal?: number;
        isOfficial?: boolean;
      };
      "com.pulsemcp/server-version"?: {
        source?: string;
        status?: string;
        publishedAt?: string;
        updatedAt?: string;
        isLatest?: boolean;
      };
    };
  };
}

function MCPServerCard({ server }: MCPServerCardProps) {
  const meta = server.meta["com.pulsemcp/server"];
  const isOfficial = meta?.isOfficial;
  const visitorsTotal = meta?.visitorsEstimateTotal;

  return (
    <Card>
      <Card.Title>
        <Stack direction="horizontal" justify="space-between">
          <Stack direction="horizontal" gap={3}>
            <div className="w-12 h-12 rounded-lg bg-primary/10 flex items-center justify-center shrink-0">
              <Type variant="subheading">{server.title[0]}</Type>
            </div>
            <Stack gap={1}>
              <Stack direction="horizontal" gap={2} align="center">
                <Type variant="subheading">{server.title}</Type>
                {isOfficial && <Badge>Official</Badge>}
              </Stack>
              <Type small muted>
                {server.name} • v{server.version}
              </Type>
            </Stack>
          </Stack>
        </Stack>
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
          <Button variant="secondary" size="sm">
            <Button.Text>Add to Project</Button.Text>
          </Button>
        </Stack>
      </Card.Footer>
    </Card>
  );
}
