import { CardGrid } from "@/components/card-grid";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { CreateResourceCard } from "@/components/create-resource-card";
import { SearchBar } from "@/components/ui/search-bar";
import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
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
        <RequireScope scope="org:admin" level="page">
          <Page.Section>
            <Page.Section.Title>Collections</Page.Section.Title>
            <Page.Section.Description>
              Collections allow you to create reusable configurations of
              multiple MCP servers to install into multiple projects in one go.
            </Page.Section.Description>
            <Page.Section.Body>
              <Stack direction="vertical" gap={4}>
                <SearchBar
                  value={searchQuery}
                  onChange={setSearchQuery}
                  placeholder="Search collections..."
                  className="w-64"
                />

                <CollectionGrid
                  collections={collections}
                  searchQuery={searchQuery}
                  onCreate={handleCreateCollection}
                />
              </Stack>
            </Page.Section.Body>
          </Page.Section>
        </RequireScope>
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
        <CardGrid>{createCard}</CardGrid>
      </div>
    );
  }

  return (
    <CardGrid>
      {collections.map((collection) => (
        <CollectionCard key={collection.id} collection={collection} />
      ))}
      {createCard}
    </CardGrid>
  );
}
