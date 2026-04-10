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
        <SearchX className="w-10 h-10 text-muted-foreground mb-3" />
        <p className="text-sm text-muted-foreground">
          No collections found. Try a different search.
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
