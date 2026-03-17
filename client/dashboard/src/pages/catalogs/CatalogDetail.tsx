import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { ExternalMCPServer } from "@gram/client/models/components";
import { Badge, Button, Stack } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Plus, Server as ServerIcon } from "lucide-react";
import { useParams } from "react-router";

export default function CatalogDetail() {
  const { registrySlug } = useParams<{ registrySlug: string }>();
  const routes = useRoutes();
  const client = useSdkClient();

  const { data: registriesData } = useQuery({
    queryKey: ["listMCPRegistries"],
    queryFn: () => client.mcpRegistries.listRegistries({}),
  });

  const registry = registriesData?.registries?.find(
    (r) => r.slug === registrySlug,
  );

  const { data: serversData, isLoading: serversLoading } = useQuery({
    queryKey: ["serveMCPRegistry", registrySlug],
    queryFn: () =>
      client.mcpRegistries.serve({
        registrySlug: registrySlug!,
      }),
    enabled: !!registrySlug,
  });

  const servers = serversData?.servers ?? [];

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <div className="mb-4">
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => routes.catalogs.goTo()}
          >
            <ArrowLeft className="w-4 h-4 mr-1" />
            <Button.Text>Back to Catalogs</Button.Text>
          </Button>
        </div>

        {registry && (
          <div className="mb-6">
            <Stack direction="horizontal" gap={3} align="center">
              <Heading variant="h4">{registry.name}</Heading>
              <Badge
                variant={
                  registry.visibility === "public" ? "success" : "neutral"
                }
              >
                {registry.visibility}
              </Badge>
            </Stack>
            {registry.slug && (
              <Type small muted className="mt-1">
                {registry.slug}
              </Type>
            )}
          </div>
        )}

        <ServersSection servers={servers} loading={serversLoading} />
      </Page.Body>
    </Page>
  );
}

function ServersSection({
  servers,
  loading,
}: {
  servers: ExternalMCPServer[];
  loading: boolean;
}) {
  if (loading) {
    return (
      <Stack direction="vertical" gap={3}>
        <Type className="font-medium">Servers</Type>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={`skeleton-${i}`} className="h-32" />
          ))}
        </div>
      </Stack>
    );
  }

  return (
    <Stack direction="vertical" gap={3}>
      <Type className="font-medium">Servers ({servers.length})</Type>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {servers.map((server) => (
          <ServerCard key={server.registrySpecifier} server={server} />
        ))}
        <AddServerCard />
      </div>
    </Stack>
  );
}

function ServerCard({ server }: { server: ExternalMCPServer }) {
  return (
    <div className="border rounded-lg p-4 bg-background hover:border-foreground/20 transition-colors">
      <Stack direction="vertical" gap={2}>
        <Stack direction="horizontal" gap={2} align="center">
          {server.iconUrl ? (
            <img src={server.iconUrl} alt="" className="w-8 h-8 rounded" />
          ) : (
            <ServerIcon className="w-5 h-5 text-muted-foreground" />
          )}
          <div className="min-w-0 flex-1">
            <Type className="font-medium truncate">
              {server.title || server.registrySpecifier}
            </Type>
          </div>
        </Stack>
        {server.description && (
          <Type small muted className="line-clamp-2">
            {server.description}
          </Type>
        )}
        <Stack direction="horizontal" gap={2} align="center">
          <Badge variant="neutral">{server.version}</Badge>
          {server.tools && server.tools.length > 0 && (
            <Type small muted>
              {server.tools.length} tool{server.tools.length !== 1 ? "s" : ""}
            </Type>
          )}
        </Stack>
      </Stack>
    </div>
  );
}

function AddServerCard() {
  return (
    <button
      type="button"
      onClick={() => {
        // no-op for now
      }}
      className="border border-dashed rounded-lg p-4 bg-background hover:border-foreground/20 transition-colors flex flex-col items-center justify-center min-h-[120px] cursor-pointer"
    >
      <Plus className="w-6 h-6 text-muted-foreground mb-2" />
      <Type small muted>
        Add More Servers
      </Type>
    </button>
  );
}
