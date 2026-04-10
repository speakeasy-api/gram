import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { getServerURL } from "@/lib/utils";

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

function configUrl(projectId: string): string {
  return `${getServerURL()}/v1/projects/${projectId}/corpus/autopublish`;
}

async function fetchConfig(projectId: string): Promise<AutoPublishConfig> {
  const res = await fetch(configUrl(projectId), { credentials: "include" });
  if (!res.ok)
    throw new Error(`Failed to fetch auto-publish config: ${res.status}`);
  return res.json();
}

async function updateConfig(
  projectId: string,
  config: AutoPublishConfig,
): Promise<AutoPublishConfig> {
  const res = await fetch(configUrl(projectId), {
    method: "PUT",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(config),
  });
  if (!res.ok)
    throw new Error(`Failed to update auto-publish config: ${res.status}`);
  return res.json();
}

/**
 * Hook for reading and writing auto-publish (YOLO mode) configuration.
 */
export function useAutoPublish(projectId: string) {
  const queryClient = useQueryClient();

  const query = useQuery<AutoPublishConfig>({
    queryKey: [QUERY_KEY, projectId],
    queryFn: () => fetchConfig(projectId),
  });

  const mutation = useMutation<AutoPublishConfig, Error, AutoPublishConfig>({
    mutationFn: (config) => updateConfig(projectId, config),
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
