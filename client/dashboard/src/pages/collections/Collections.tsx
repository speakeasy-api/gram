import { Page } from "@/components/page-layout";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useOrgRoutes } from "@/routes";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import { Plus, Search, SearchX, X } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { useCollections } from "./hooks";
import { CollectionCard } from "./CollectionCard";
import type { Collection } from "./types";

export function CollectionsRoot() {
  return <Outlet />;
}

export default function Collections() {
  const orgRoutes = useOrgRoutes();
  const [tab, setTab] = useState<"discover" | "org">("org");
  const [searchQuery, setSearchQuery] = useState("");

  const { data: collections } = useCollections(tab, searchQuery);
  const { data: orgCollections } = useCollections("org");
  const orgCount = orgCollections.length;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Collections</Page.Section.Title>
          <Page.Section.Description>
            Collections allow you to create reusable configurations of multiple
            MCP servers to install into multiple projects in one go.
          </Page.Section.Description>
          <Page.Section.CTA>
            <Button onClick={() => orgRoutes.collections.create.goTo()}>
              <Plus className="w-4 h-4 mr-2" />
              Create Collection
            </Button>
          </Page.Section.CTA>
          <Page.Section.Body>
            <Tabs
              value={tab}
              onValueChange={(v) => setTab(v as "discover" | "org")}
            >
              <Stack
                direction="horizontal"
                gap={3}
                align="center"
                justify="space-between"
                className="mb-4"
              >
                <TabsList>
                  <TabsTrigger value="org">
                    My Organization
                    <span className="ml-1.5 px-1.5 py-0.5 rounded-full bg-muted-foreground/10 text-xs font-medium tabular-nums">
                      {orgCount}
                    </span>
                  </TabsTrigger>
                  <TabsTrigger value="discover">Discover</TabsTrigger>
                </TabsList>

                <div className="relative w-64">
                  <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                  <Input
                    placeholder="Search collections..."
                    value={searchQuery}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setSearchQuery(e.target.value)
                    }
                    className="pl-10 pr-9 h-10"
                  />
                  {searchQuery && (
                    <button
                      onClick={() => setSearchQuery("")}
                      className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                      aria-label="Clear search"
                    >
                      <X className="w-4 h-4" />
                    </button>
                  )}
                </div>
              </Stack>

              <TabsContent value="discover">
                <CollectionGrid collections={collections} />
              </TabsContent>

              <TabsContent value="org">
                <CollectionGrid collections={collections} />
              </TabsContent>
            </Tabs>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function CollectionGrid({ collections }: { collections: Collection[] }) {
  if (collections.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center">
        <SearchX className="w-10 h-10 text-muted-foreground mb-3" />
        <p className="text-sm text-muted-foreground">
          No collections found. Try a different search or tab.
        </p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
      {collections.map((collection) => (
        <CollectionCard key={collection.id} collection={collection} />
      ))}
    </div>
  );
}
