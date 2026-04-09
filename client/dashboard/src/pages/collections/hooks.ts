import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import type { ExternalMCPServer } from "@gram/client/models/components";
import {
  invalidateAllListMCPCollections,
  invalidateAllMcpRegistriesServe,
  useListMCPCollections,
  useMcpRegistriesAttachServerMutation,
  useMcpRegistriesCreateCollectionMutation,
  useMcpRegistriesDeleteCollectionMutation,
  useMcpRegistriesDetachServerMutation,
  useMcpRegistriesServe,
  useMcpRegistriesUpdateCollectionMutation,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Collection, CollectionServer } from "./types";

function useDefaultProjectSlug(): string | undefined {
  const organization = useOrganization();
  return organization.projects?.[0]?.slug;
}

function useInvalidateCollections() {
  const queryClient = useQueryClient();
  return () => {
    invalidateAllListMCPCollections(queryClient);
    invalidateAllMcpRegistriesServe(queryClient);
  };
}

export function useCollections(search?: string): {
  data: Collection[];
  isLoading: boolean;
} {
  const projectSlug = useDefaultProjectSlug();

  const { data, isLoading } = useListMCPCollections(
    { gramProject: projectSlug },
    undefined,
    { enabled: !!projectSlug },
  );

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
  const projectSlug = useDefaultProjectSlug();

  const { data, isLoading } = useMcpRegistriesServe(
    { collectionSlug: slug!, gramProject: projectSlug },
    undefined,
    { enabled: !!slug && !!projectSlug },
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

export function useUpdateCollection() {
  const projectSlug = useDefaultProjectSlug();
  const invalidate = useInvalidateCollections();

  return useMcpRegistriesUpdateCollectionMutation({
    onSuccess: () => {
      invalidate();
      toast.success("Collection updated");
    },
    onError: (err) => {
      toast.error(`Failed to update collection: ${err.message}`);
    },
  });
}

export function useDeleteCollection() {
  const invalidate = useInvalidateCollections();

  return useMcpRegistriesDeleteCollectionMutation({
    onSuccess: () => {
      invalidate();
      toast.success("Collection deleted");
    },
    onError: (err) => {
      toast.error(`Failed to delete collection: ${err.message}`);
    },
  });
}

export function useAttachServer() {
  const invalidate = useInvalidateCollections();

  return useMcpRegistriesAttachServerMutation({
    onSuccess: () => {
      invalidate();
      toast.success("Server added to collection");
    },
    onError: (err) => {
      toast.error(`Failed to add server: ${err.message}`);
    },
  });
}

export function useDetachServer() {
  const invalidate = useInvalidateCollections();

  return useMcpRegistriesDetachServerMutation({
    onSuccess: () => {
      invalidate();
      toast.success("Server removed from collection");
    },
    onError: (err) => {
      toast.error(`Failed to remove server: ${err.message}`);
    },
  });
}

export function useCreateCollection() {
  const invalidate = useInvalidateCollections();

  return useMcpRegistriesCreateCollectionMutation({
    onSuccess: () => {
      invalidate();
    },
    onError: (err) => {
      toast.error(`Failed to create collection: ${err.message}`);
    },
  });
}
