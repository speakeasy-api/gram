import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Collection, CollectionServer } from "./types";

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

/**
 * At the org level there's no project slug in the URL, so the SDK HTTP client
 * won't set the Gram-Project header automatically. We grab the first project
 * from the org and pass it explicitly in the request headers.
 */
function useDefaultProjectSlug(): string | undefined {
  const organization = useOrganization();
  return organization.projects?.[0]?.slug;
}

export function useCreateCollection() {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const projectSlug = useDefaultProjectSlug();

  return useMutation({
    mutationFn: (params: {
      name: string;
      slug?: string;
      toolsetIds: string[];
      visibility: "public" | "private";
    }) =>
      client.mcpRegistries.publish({
        gramProject: projectSlug,
        publishRequestBody: {
          name: params.name,
          slug: params.slug || slugify(params.name),
          toolsetIds: params.toolsetIds,
          visibility: params.visibility,
        },
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ["collections"],
      });
    },
    onError: (err) => {
      toast.error(`Failed to create collection: ${err.message}`);
    },
  });
}

export function useCollections(
  tab?: "discover" | "org",
  search?: string,
): {
  data: Collection[];
  isLoading: boolean;
} {
  const client = useSdkClient();
  const projectSlug = useDefaultProjectSlug();

  const { data: registriesData, isLoading: registriesLoading } = useQuery({
    queryKey: ["collections", "list", projectSlug],
    queryFn: () =>
      client.mcpRegistries.listRegistries({ gramProject: projectSlug }),
    enabled: !!projectSlug,
  });

  const organization = useOrganization();
  const orgId = organization.id;

  const registries = (registriesData?.registries ?? []).filter(
    (r) => r.source === "internal",
  );

  const filtered =
    tab === "discover"
      ? registries.filter((r) => r.visibility === "public")
      : registries.filter((r) => r.organizationId === orgId);

  const searched = search
    ? filtered.filter(
        (r) =>
          r.name.toLowerCase().includes(search.toLowerCase()) ||
          r.slug?.toLowerCase().includes(search.toLowerCase()),
      )
    : filtered;

  const collections: Collection[] = searched.map((r) => ({
    id: r.id,
    name: r.name,
    slug: r.slug,
    description: "",
    visibility: (r.visibility as "public" | "private") ?? "private",
    servers: [],
    author: { orgName: "", orgId: r.organizationId ?? "" },
    installCount: 0,
    createdAt: "",
    updatedAt: "",
  }));

  return { data: collections, isLoading: registriesLoading };
}

export function useCollectionDetail(idOrSlug: string): {
  data: Collection | null;
  isLoading: boolean;
} {
  const client = useSdkClient();
  const projectSlug = useDefaultProjectSlug();

  const { data: registriesData, isLoading: registriesLoading } = useQuery({
    queryKey: ["collections", "list", projectSlug],
    queryFn: () =>
      client.mcpRegistries.listRegistries({ gramProject: projectSlug }),
    enabled: !!projectSlug,
  });

  const registry = (registriesData?.registries ?? []).find(
    (r) => r.slug === idOrSlug || r.id === idOrSlug,
  );

  const { data: serveData, isLoading: serveLoading } = useQuery({
    queryKey: ["collections", "serve", registry?.slug, projectSlug],
    queryFn: () =>
      client.mcpRegistries.serve({
        registrySlug: registry!.slug!,
        gramProject: projectSlug,
      }),
    enabled: !!registry?.slug && !!projectSlug,
  });

  if (registriesLoading || serveLoading) {
    return { data: null, isLoading: true };
  }

  if (!registry) {
    return { data: null, isLoading: false };
  }

  const servers: CollectionServer[] = (serveData?.servers ?? []).map((s) => ({
    registrySpecifier: s.registrySpecifier ?? "",
    title: s.title ?? s.registrySpecifier ?? "",
    description: s.description ?? "",
    iconUrl: s.iconUrl ?? undefined,
    toolCount: s.tools?.length ?? 0,
  }));

  const collection: Collection = {
    id: registry.id,
    name: registry.name,
    slug: registry.slug,
    description: "",
    visibility: (registry.visibility as "public" | "private") ?? "private",
    servers,
    author: { orgName: "", orgId: registry.organizationId ?? "" },
    installCount: 0,
    createdAt: "",
    updatedAt: "",
  };

  return { data: collection, isLoading: false };
}

export function useCollectionServers(slug: string | undefined): {
  servers: CollectionServer[];
  isLoading: boolean;
} {
  const client = useSdkClient();
  const projectSlug = useDefaultProjectSlug();

  const { data, isLoading } = useQuery({
    queryKey: ["collections", "serve", slug, projectSlug],
    queryFn: () =>
      client.mcpRegistries.serve({
        registrySlug: slug!,
        gramProject: projectSlug,
      }),
    enabled: !!slug && !!projectSlug,
  });

  const servers: CollectionServer[] = (data?.servers ?? []).map((s) => ({
    registrySpecifier: s.registrySpecifier ?? "",
    title: s.title ?? s.registrySpecifier ?? "",
    description: s.description ?? "",
    iconUrl: s.iconUrl ?? undefined,
    toolCount: s.tools?.length ?? 0,
  }));

  return { servers, isLoading };
}

export function useInstallCollection() {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: {
      collectionId: string;
      collectionSlug: string;
      projectSlug: string;
    }) => {
      // 1. Fetch servers in the collection
      const serveResult = await client.mcpRegistries.serve({
        registrySlug: params.collectionSlug,
        gramProject: params.projectSlug,
      });

      const servers = serveResult.servers ?? [];
      if (servers.length === 0) {
        throw new Error("Collection has no servers to install");
      }

      // 2. Evolve the deployment with external MCP attachments
      return client.deployments.evolveDeployment({
        gramProject: params.projectSlug,
        evolveForm: {
          nonBlocking: true,
          upsertExternalMcps: servers.map((server) => ({
            registryId: params.collectionId,
            name: server.title || server.registrySpecifier || "",
            slug: server.registrySpecifier?.split("/").pop() || "",
            registryServerSpecifier: server.registrySpecifier || "",
          })),
        },
      });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["deployments"] });
    },
    onError: (err) => {
      toast.error(`Failed to install collection: ${err.message}`);
    },
  });
}
