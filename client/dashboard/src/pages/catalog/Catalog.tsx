import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import {
  Server,
  useSampleListRegistry,
} from "@/pages/catalog/useSampleListRegistry";
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
  const [addingServer, setAddingServer] = useState<Server | null>(null);
  const [desiredToolsetName, setDesiredToolsetName] = useState("");

  useEffect(() => {
    setDesiredToolsetName(addingServer?.title ?? "");
  }, [addingServer]);

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

  const addServerToProject = () => {
    //TODO create source
    //TODO create toolset
  };

  const addToProjectDialog = (
    <Dialog
      open={addingServer !== null}
      onOpenChange={(open) => setAddingServer(open ? addingServer : null)}
    >
      <Dialog.Content>
        <Dialog.Header>
          {addingServer && <ServerHeading server={addingServer} />}
          <Dialog.Description className="border-t pt-3">
            Import this MCP server into your project. This will create a new
            toolset and corresponding MCP server.
          </Dialog.Description>
        </Dialog.Header>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            addServerToProject();
          }}
          className="flex flex-col gap-2 mt-2 mb-4"
        >
          <Label>Toolset Name</Label>
          <Input
            placeholder="Toolset name"
            value={desiredToolsetName}
            onChange={(e) => setDesiredToolsetName(e.target.value)}
            required
          />
        </form>
        <Dialog.Footer>
          <Button variant="secondary" onClick={() => setAddingServer(null)}>
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={desiredToolsetName.length === 0}
            onClick={() => addServerToProject()}
          >
            Add
          </Button>
        </Dialog.Footer>
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
                  filteredServers?.map((server) => (
                    <MCPServerCard
                      key={server.registry_id}
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

              {!isLoading && filteredServers?.length === 0 && (
                <div className="text-center py-12">
                  <Type variant="subheading" className="text-muted-foreground">
                    No MCP servers found matching "{searchQuery}"
                  </Type>
                </div>
              )}
            </Stack>
            {addToProjectDialog}
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

interface MCPServerCardProps {
  server: Server;
  onAddToProject: (server: Server) => void;
}

function ServerHeading({ server }: { server: Server }) {
  const meta = server.meta["com.pulsemcp/server"];
  const isOfficial = meta?.isOfficial;

  return (
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
  );
}

function MCPServerCard({ server, onAddToProject }: MCPServerCardProps) {
  const meta = server.meta["com.pulsemcp/server"];
  const visitorsTotal = meta?.visitorsEstimateTotal;

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
