import type { Gram } from "@gram/client";
import {
  useCorpusSetAutoPublishConfigMutation,
  useGramContext,
} from "@gram/client/react-query";
import { useQuery, useQueryClient } from "@tanstack/react-query";

/**
 * Shape of the auto-publish configuration returned by the server.
 */
export interface AutoPublishConfig {
  enabled: boolean;
  intervalMinutes: number;
  minUpvotes: number;
  authorTypeFilter: string | null;
  labelFilter: string[] | null;
  minAgeHours: number;
}

const QUERY_KEY = "autoPublishConfig";

/**
 * Hook for reading and writing auto-publish (YOLO mode) configuration.
 */
export function useAutoPublish(projectId: string) {
  const client = useGramContext() as Gram;
  const queryClient = useQueryClient();

  const query = useQuery<AutoPublishConfig>({
    queryKey: [QUERY_KEY, projectId],
    queryFn: async () => {
      const config = await client.corpus.getAutoPublishConfig({});
      return {
        authorTypeFilter: config.authorTypeFilter ?? null,
        enabled: config.enabled,
        intervalMinutes: config.intervalMinutes,
        labelFilter: config.labelFilter ?? null,
        minAgeHours: config.minAgeHours,
        minUpvotes: config.minUpvotes,
      };
    },
  });

  const mutation = useCorpusSetAutoPublishConfigMutation({
    onSuccess: (data) => {
      queryClient.setQueryData([QUERY_KEY, projectId], {
        authorTypeFilter: data.authorTypeFilter ?? null,
        enabled: data.enabled,
        intervalMinutes: data.intervalMinutes,
        labelFilter: data.labelFilter ?? null,
        minAgeHours: data.minAgeHours,
        minUpvotes: data.minUpvotes,
      } satisfies AutoPublishConfig);
    },
  });

  return {
    config: query.data ?? null,
    isLoading: query.isLoading,
    error: query.error,
    setConfig: (config: AutoPublishConfig) =>
      mutation.mutate({
        request: {
          corpusAutoPublishConfigResult: {
            authorTypeFilter: config.authorTypeFilter ?? undefined,
            enabled: config.enabled,
            intervalMinutes: config.intervalMinutes,
            labelFilter: config.labelFilter ?? undefined,
            minAgeHours: config.minAgeHours,
            minUpvotes: config.minUpvotes,
          },
        },
      }),
    isSaving: mutation.isPending,
  };
}
