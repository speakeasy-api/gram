import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { CreateResourceCard } from "@/components/create-resource-card";
import { Text } from "@/components/ui/Text";
import { useState } from "react";
import { Outlet, useNavigate } from "react-router";
import { useCollections } from "./hooks";
import { CollectionCard } from "./CollectionCard";
import type { Collection } from "./types";

export function CollectionsRoot(): JSX.Element {
  return <Outlet />;
}

export default function Collections(): JSX.Element {
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <CollectionsInner />
    </RequireScope>
  );
}

function CollectionsInner() {
  const [searchQuery, setSearchQuery] = useState("");
  const { data: collections } = useCollections(searchQuery);
  const navigate = useNavigate();
  const handleCreateCollection = () => navigate("create");

  return (
    <ResourceListPage
      title="Collections"
      description="Collections allow you to create reusable groups of MCP servers and skills to install into multiple projects in one go."
      search={{
        value: searchQuery,
        onChange: setSearchQuery,
        placeholder: "Search collections...",
      }}
    >
      <CollectionGrid
        collections={collections}
        searchQuery={searchQuery}
        onCreate={() => void handleCreateCollection()}
      />
    </ResourceListPage>
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
    <RequireScope scope="org:admin" level="section">
      <CreateResourceCard
        title="New Collection"
        description="Create a reusable collection of MCP servers for your organization"
        onClick={onCreate}
      />
    </RequireScope>
  );

  if (collections.length === 0) {
    return (
      <div className="space-y-4">
        {searchQuery ? (
          <Text muted>No collections matching &ldquo;{searchQuery}&rdquo;</Text>
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
