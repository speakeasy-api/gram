import { Page } from "@/components/page-layout";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import { Plus, Search, SearchX, X } from "lucide-react";
import { useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { useCollections } from "./hooks";
import { CollectionCard } from "./CollectionCard";
import type { Collection } from "./types";

export function CollectionsRoot() {
  return <Outlet />;
}

export default function Collections() {
  const [searchQuery, setSearchQuery] = useState("");
  const { data: collections } = useCollections(searchQuery);
  const navigate = useNavigate();

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
          <Page.Section.Body>
            <Stack direction="vertical" gap={4}>
              <div className="flex items-center gap-3">
                <div className="relative w-64">
                  <Search className="text-muted-foreground absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2" />
                  <Input
                    placeholder="Search collections..."
                    value={searchQuery}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setSearchQuery(e.target.value)
                    }
                    className="h-10 pr-9 pl-10"
                  />
                  {searchQuery && (
                    <button
                      onClick={() => setSearchQuery("")}
                      className="text-muted-foreground hover:text-foreground absolute top-1/2 right-3 -translate-y-1/2 transition-colors"
                      aria-label="Clear search"
                    >
                      <X className="h-4 w-4" />
                    </button>
                  )}
                </div>
                <Button onClick={() => navigate("create")}>
                  <Button.Icon>
                    <Plus />
                  </Button.Icon>
                  <Button.Text>Create Collection</Button.Text>
                </Button>
              </div>

              <CollectionGrid collections={collections} />
            </Stack>
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
        <SearchX className="text-muted-foreground mb-3 h-10 w-10" />
        <p className="text-muted-foreground text-sm">
          No collections found. Try a different search.
        </p>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      {collections.map((collection) => (
        <CollectionCard key={collection.id} collection={collection} />
      ))}
    </div>
  );
}
