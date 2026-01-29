import { useSdkClient } from "@/contexts/Sdk";
import { ExternalMCPServer } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query";
import { queryKeyListMCPCatalog } from "@gram/client/react-query";
import { useInfiniteQuery, useMutation } from "@tanstack/react-query";
import { useEffect, useState } from "react";

export function generateSlug(name: string): string {
  const lastPart = name.split("/").pop() || name;
  return lastPart
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export function useAddServerMutation() {
  const client = useSdkClient();
  const { data: deploymentResult, refetch: refetchDeployment } =
    useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const mutation = useMutation({
    mutationFn: async ({
      server,
      toolsetName,
    }: {
      server: ExternalMCPServer;
      toolsetName: string;
    }) => {
      const slug = generateSlug(server.registrySpecifier);
      let toolUrns = [`tools:externalmcp:${slug}:proxy`];
      if (server.tools) {
        toolUrns = server.tools.map(
          (t) => `tools:externalmcp:${slug}:${t.name}`,
        );
      }

      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          upsertExternalMcps: [
            {
              registryId: server.registryId,
              name: toolsetName,
              slug,
              registryServerSpecifier: server.registrySpecifier,
            },
          ],
        },
      });

      const toolset = await client.toolsets.create({
        createToolsetRequestBody: {
          name: toolsetName,
          description:
            server.description ?? `MCP server: ${server.registrySpecifier}`,
          toolUrns,
        },
      });

      await client.toolsets.updateBySlug({
        slug: toolset.slug,
        updateToolsetRequestBody: {
          mcpEnabled: true,
          mcpIsPublic: true,
        },
      });

      // Fetch the toolset to get the generated mcpSlug
      const updatedToolset = await client.toolsets.getBySlug({
        slug: toolset.slug,
      });

      return {
        slug: toolset.slug,
        mcpSlug: updatedToolset.mcpSlug,
      };
    },
  });

  return { mutation, refetchDeployment };
}

interface ServerMeta {
  "com.pulsemcp/server"?: {
    visitorsEstimateMostRecentWeek?: number;
    visitorsEstimateLastFourWeeks?: number;
    visitorsEstimateTotal?: number;
    isOfficial?: boolean;
  };
  "com.pulsemcp/server-version"?: {
    source?: string;
    status?: string;
    publishedAt?: string;
    updatedAt?: string;
    isLatest?: boolean;
    "remotes[0]"?: {
      tools?: Array<{
        name: string;
        description?: string;
        annotations?: {
          title?: string;
          readOnlyHint?: boolean;
          destructiveHint?: boolean;
        };
      }>;
      auth?: {
        type?: string;
      };
    };
  };
}

export type Server = ExternalMCPServer & {
  meta: ServerMeta;
};

export function useInfiniteListMCPCatalog(
  search?: string,
  registryId?: string,
) {
  const client = useSdkClient();
  const [debouncedSearch, setDebouncedSearch] = useState(search);

  useEffect(() => {
    const handler = setTimeout(() => {
      setDebouncedSearch(search);
    }, 300);

    return () => {
      clearTimeout(handler);
    };
  }, [search]);

  return useInfiniteQuery({
    queryKey: queryKeyListMCPCatalog({ search: debouncedSearch, registryId }),
    queryFn: async ({ pageParam }) => {
      return client.mcpRegistries.listCatalog({
        search: debouncedSearch || undefined,
        registryId: registryId || undefined,
        cursor: pageParam,
      });
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    staleTime: 5 * 60 * 1000, // 5 minutes - won't refetch if data is fresh
  });
}
