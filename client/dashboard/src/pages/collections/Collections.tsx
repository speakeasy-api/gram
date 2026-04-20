import { Page } from "@/components/page-layout";
import { CreateResourceCard } from "@/components/create-resource-card";
import { Type } from "@/components/ui/type";
import { Input, Stack } from "@speakeasy-api/moonshine";
import { Search, X } from "lucide-react";
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
  const handleCreateCollection = () => navigate("create");

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
              </div>

              <CollectionGrid
                collections={collections}
                searchQuery={searchQuery}
                onCreate={handleCreateCollection}
              />
            </Stack>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

function CollectionGrid({
  collections,
  searchQuery,
  onCreate,
}: {
  collections: Collection[];
  searchQuery: string;
  onCreate: () => void;
}) {
  const createCard = (
    <CreateResourceCard
      title="New Collection"
      description="Create a reusable collection of MCP servers for your organization"
      onClick={onCreate}
    />
  );

  if (collections.length === 0) {
    return (
      <div className="space-y-4">
        {searchQuery ? (
          <Type muted>No collections matching &ldquo;{searchQuery}&rdquo;</Type>
        ) : null}
        <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
          {createCard}
        </div>
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
      {collections.map((collection) => (
        <CollectionCard key={collection.id} collection={collection} />
      ))}
      {createCard}
    </div>
  );
}
