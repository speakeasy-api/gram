import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { rpc } from "@/lib/rpc";

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

async function fetchConfig(): Promise<AutoPublishConfig> {
  return rpc<AutoPublishConfig>("corpus.getAutoPublishConfig", {});
}

async function updateConfig(
  config: AutoPublishConfig,
): Promise<AutoPublishConfig> {
  return rpc<AutoPublishConfig>("corpus.setAutoPublishConfig", {
    enabled: config.enabled,
    interval_minutes: config.intervalMinutes,
    min_upvotes: config.minUpvotes,
    author_type_filter: config.authorTypeFilter,
    label_filter: config.labelFilter,
    min_age_hours: config.minAgeHours,
  });
}

/**
 * Hook for reading and writing auto-publish (YOLO mode) configuration.
 */
export function useAutoPublish(projectId: string) {
  const queryClient = useQueryClient();

  const query = useQuery<AutoPublishConfig>({
    queryKey: [QUERY_KEY, projectId],
    queryFn: () => fetchConfig(),
  });

  const mutation = useMutation<AutoPublishConfig, Error, AutoPublishConfig>({
    mutationFn: updateConfig,
    onSuccess: (data) => {
      queryClient.setQueryData([QUERY_KEY, projectId], data);
    },
  });

  return {
    config: query.data ?? null,
    isLoading: query.isLoading,
    error: query.error,
    setConfig: mutation.mutate,
    isSaving: mutation.isPending,
  };
}
