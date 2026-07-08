import type { ExternalMCPServer } from "@gram/client/models/components/externalmcpserver.js";
import { useCollectionsAttachServerMutation } from "@gram/client/react-query/collectionsAttachServer.js";
import { useCollectionsCreateMutation } from "@gram/client/react-query/collectionsCreate.js";
import { useCollectionsDeleteMutation } from "@gram/client/react-query/collectionsDelete.js";
import { useCollectionsDetachServerMutation } from "@gram/client/react-query/collectionsDetachServer.js";
import {
  invalidateAllCollectionsListServers,
  useCollectionsListServers,
} from "@gram/client/react-query/collectionsListServers.js";
import { useCollectionsUpdateMutation } from "@gram/client/react-query/collectionsUpdate.js";
import {
  invalidateAllListCollections,
  useListCollections,
} from "@gram/client/react-query/listCollections.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Collection, CollectionServer } from "./types";

function useInvalidateCollections() {
  const queryClient = useQueryClient();
  return () => {
    void invalidateAllListCollections(queryClient);
    void invalidateAllCollectionsListServers(queryClient);
  };
}

export function useCollections(search?: string): {
  data: Collection[];
  isLoading: boolean;
} {
  const { data, isLoading } = useListCollections({}, undefined);

  const registries = data?.collections ?? [];

  const searched = search
    ? registries.filter(
        (r) =>
          r.name.toLowerCase().includes(search.toLowerCase()) ||
          r.slug?.toLowerCase().includes(search.toLowerCase()),
      )
    : registries;

  const collections: Collection[] = searched.map((r) => ({
    id: r.id,
    name: r.name,
    slug: r.slug,
    description: r.description ?? "",
    visibility: (r.visibility as "public" | "private") ?? "private",
    servers: [],
    author: { orgName: "", orgId: "" },
    installCount: 0,
    createdAt: "",
    updatedAt: "",
  }));

  return { data: collections, isLoading };
}

export function useCollectionServers(slug: string | undefined): {
  servers: CollectionServer[];
  rawServers: ExternalMCPServer[];
  isLoading: boolean;
} {
  const { data, isLoading } = useCollectionsListServers(
    { collectionSlug: slug! },
    undefined,
    { enabled: !!slug },
  );

  const rawServers: ExternalMCPServer[] = data?.servers ?? [];

  const servers: CollectionServer[] = rawServers.map((s) => ({
    registrySpecifier: s.registrySpecifier ?? "",
    title: s.title ?? s.registrySpecifier ?? "",
    description: s.description ?? "",
    iconUrl: s.iconUrl ?? undefined,
    toolCount: s.tools?.length ?? 0,
  }));

  return { servers, rawServers, isLoading };
}

export function useUpdateCollection(): ReturnType<
  typeof useCollectionsUpdateMutation
> {
  const invalidate = useInvalidateCollections();

  return useCollectionsUpdateMutation({
    onSuccess: () => {
      invalidate();
      toast.success("Collection updated");
    },
    onError: (err) => {
      toast.error(`Failed to update collection: ${err.message}`);
    },
  });
}

export function useDeleteCollection(): ReturnType<
  typeof useCollectionsDeleteMutation
> {
  const invalidate = useInvalidateCollections();

  return useCollectionsDeleteMutation({
    onSuccess: () => {
      invalidate();
      toast.success("Collection deleted");
    },
    onError: (err) => {
      toast.error(`Failed to delete collection: ${err.message}`);
    },
  });
}

export function useAttachServer(): ReturnType<
  typeof useCollectionsAttachServerMutation
> {
  const invalidate = useInvalidateCollections();

  return useCollectionsAttachServerMutation({
    onSuccess: () => {
      invalidate();
      toast.success("Server added to collection");
    },
    onError: (err) => {
      toast.error(`Failed to add server: ${err.message}`);
    },
  });
}

export function useDetachServer(): ReturnType<
  typeof useCollectionsDetachServerMutation
> {
  const invalidate = useInvalidateCollections();

  return useCollectionsDetachServerMutation({
    onSuccess: () => {
      invalidate();
      toast.success("Server removed from collection");
    },
    onError: (err) => {
      toast.error(`Failed to remove server: ${err.message}`);
    },
  });
}

export function useCreateCollection(): ReturnType<
  typeof useCollectionsCreateMutation
> {
  const invalidate = useInvalidateCollections();

  return useCollectionsCreateMutation({
    onSuccess: () => {
      invalidate();
    },
    onError: (err) => {
      toast.error(`Failed to create collection: ${err.message}`);
    },
  });
}
