import { Page } from "@/components/page-layout";
import { Type } from "@/components/ui/type";
import { useSampleListRegistry } from "@/hooks/useSampleListRegistry";
import {
  Badge,
  Button,
  Input,
  Skeleton,
  Stack,
} from "@speakeasy-api/moonshine";
import { Search } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";

export function CatalogRoot() {
  return <Outlet />;
}

export default function Catalog() {
  const [searchQuery, setSearchQuery] = useState("");
  const { data, isLoading } = useSampleListRegistry();

  const filteredServers = data?.servers.filter((server) => {
    const query = searchQuery.toLowerCase();
    return (
      server.title.toLowerCase().includes(query) ||
      server.name.toLowerCase().includes(query) ||
      server.description.toLowerCase().includes(query)
    );
  });

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
            MCP server registry.
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

              {isLoading ? (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                  {[...Array(6)].map((_, i) => (
                    <Skeleton key={i} className="h-[200px]" />
                  ))}
                </div>
              ) : (
                <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
                  {filteredServers?.map((server) => (
                    <MCPServerCard key={server.name} server={server} />
                  ))}
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
  const isOfficial = server.meta["com.pulsemcp/server"]?.isOfficial;
  const visitorsTotal =
    server.meta["com.pulsemcp/server"]?.visitorsEstimateTotal;

  return (
    <div className="border rounded-lg p-6 bg-card hover:bg-accent/50 transition-colors">
      <Stack direction="vertical" gap={4}>
        <Stack direction="horizontal" justify="space-between" align="start">
          <Stack direction="horizontal" gap={3} align="start">
            <div className="w-12 h-12 rounded-lg bg-primary/10 flex items-center justify-center flex-shrink-0">
              <Type variant="subheading">{server.title[0]}</Type>
            </div>
            <Stack direction="vertical" gap={1}>
              <Stack direction="horizontal" gap={2} align="center">
                <Type variant="subheading">{server.title}</Type>
                {isOfficial && (
                  <Badge variant="default" size="sm">
                    Official
                  </Badge>
                )}
              </Stack>
              <Type small className="text-muted-foreground">
                {server.name} • v{server.version}
              </Type>
            </Stack>
          </Stack>
        </Stack>

        <Type small className="text-foreground line-clamp-2">
          {server.description}
        </Type>

        <Stack direction="horizontal" justify="space-between" align="center">
          {visitorsTotal ? (
            <Type small className="text-muted-foreground">
              {visitorsTotal.toLocaleString()} visitors
            </Type>
          ) : (
            <div />
          )}
          <Button variant="outline" size="sm">
            <Button.Text>View Details</Button.Text>
          </Button>
        </Stack>
      </Stack>
    </div>
  );
}
