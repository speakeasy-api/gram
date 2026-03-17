import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { Badge, Button, Stack } from "@speakeasy-api/moonshine";
import { Plus, Store } from "lucide-react";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CreateCatalogDialog } from "./CreateCatalogDialog";
import { Outlet } from "react-router";

export function CatalogsRoot() {
  return <Outlet />;
}

export default function Catalogs() {
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const [isCreateOpen, setIsCreateOpen] = useState(false);

  const isEnabled = telemetry.isFeatureEnabled("catalogs");

  const { data, isLoading, refetch } = useQuery({
    queryKey: ["listMCPRegistries"],
    queryFn: () => client.mcpRegistries.listRegistries({}),
    enabled: isEnabled !== false,
  });

  const registries = data?.registries ?? [];
  const internalRegistries = registries.filter((r) => r.source === "internal");

  if (isEnabled === false) {
    return null;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Catalogs</Page.Section.Title>
          <Page.Section.Description>
            Manage your internal MCP server catalogs. Publish collections of
            toolsets as discoverable MCP servers.
          </Page.Section.Description>
          <Page.Section.Body>
            <Stack direction="vertical" gap={6}>
              <Stack
                direction="horizontal"
                gap={3}
                justify="space-between"
                align="center"
              >
                <Type small muted>
                  {internalRegistries.length} catalog
                  {internalRegistries.length !== 1 ? "s" : ""}
                </Type>
                <Button onClick={() => setIsCreateOpen(true)} size="sm">
                  <Plus className="w-4 h-4 mr-1" />
                  <Button.Text>New Catalog</Button.Text>
                </Button>
              </Stack>

              {isLoading && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {Array.from({ length: 4 }, (_, i) => (
                    <Skeleton key={`skeleton-${i}`} className="h-32" />
                  ))}
                </div>
              )}

              {!isLoading && internalRegistries.length === 0 && (
                <div className="flex flex-col items-center justify-center py-16 border rounded-xl bg-background">
                  <Store className="w-12 h-12 text-muted-foreground mb-4" />
                  <Type className="font-medium mb-1">No catalogs yet</Type>
                  <Type small muted className="mb-4">
                    Create a catalog to publish your toolsets as discoverable
                    MCP servers.
                  </Type>
                  <Button onClick={() => setIsCreateOpen(true)} size="sm">
                    <Plus className="w-4 h-4 mr-1" />
                    <Button.Text>Create Catalog</Button.Text>
                  </Button>
                </div>
              )}

              {!isLoading && internalRegistries.length > 0 && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {internalRegistries.map((registry) => (
                    <button
                      type="button"
                      key={registry.id}
                      className="border rounded-lg p-4 bg-background hover:border-foreground/20 transition-colors text-left cursor-pointer"
                      onClick={() => {
                        if (registry.slug) {
                          routes.catalogs.detail.goTo(registry.slug);
                        }
                      }}
                    >
                      <Stack direction="vertical" gap={2}>
                        <Stack direction="horizontal" gap={2} align="center">
                          <Store className="w-5 h-5 text-muted-foreground" />
                          <Type className="font-medium">{registry.name}</Type>
                          <Badge
                            variant={
                              registry.visibility === "public"
                                ? "success"
                                : "neutral"
                            }
                          >
                            {registry.visibility}
                          </Badge>
                        </Stack>
                        {registry.slug && (
                          <Type small muted>
                            {registry.slug}
                          </Type>
                        )}
                      </Stack>
                    </button>
                  ))}
                </div>
              )}
            </Stack>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>

      <CreateCatalogDialog
        open={isCreateOpen}
        onOpenChange={setIsCreateOpen}
        onCreated={() => {
          refetch();
          setIsCreateOpen(false);
        }}
      />
    </Page>
  );
}
