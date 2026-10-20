import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { queryKeyAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useQueries } from "@tanstack/react-query";
import { useMemo } from "react";

export interface AssistantGroup {
  projectId: string;
  projectName: string;
  assistants: { id: string; name: string }[];
}

export interface OrgAssistants {
  /**
   * The org's assistants grouped by project, or empty until every project's
   * listing has succeeded: a picker narrowing a grant against a partial
   * inventory could not show what the grant already names.
   */
  groups: AssistantGroup[];
  /** Whether `groups` is authoritative. */
  settled: boolean;
  /** Any project's listing failed. Always false when disabled. */
  isError: boolean;
  refetch: () => void;
}

/**
 * The assistants an `assistant:*` grant can name. Listing is project-scoped,
 * so this reads each of the organization's projects.
 */
export function useOrgAssistants(enabled: boolean): OrgAssistants {
  const organization = useOrganization();
  const sdk = useSdkClient();
  const projects = organization.projects;

  const reads = useQueries({
    queries: projects.map((project) => ({
      // Project slugs repeat across organizations, so the slug alone would
      // let one organization's cached listing answer for another's.
      queryKey: [
        ...queryKeyAssistantsList({ gramProject: project.slug }),
        organization.id,
      ],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.assistants.list({ gramProject: project.slug }, undefined, {
          signal,
        }),
      enabled,
      retry: false,
      throwOnError: false,
    })),
  });

  const settled = enabled && reads.every((read) => read.isSuccess);
  const isError = enabled && reads.some((read) => read.isError);
  const dataKey = reads.map((read) => read.dataUpdatedAt).join(",");

  const groups = useMemo((): AssistantGroup[] => {
    if (!settled) return [];
    return projects.flatMap((project, index) => {
      const assistants = (reads[index]?.data?.assistants ?? []).map(
        (assistant) => ({ id: assistant.id, name: assistant.name }),
      );
      if (assistants.length === 0) return [];
      return [{ projectId: project.id, projectName: project.name, assistants }];
    });
    // `reads` is a new array every render; `dataKey` tracks its contents.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settled, projects, dataKey]);

  return {
    groups,
    settled,
    isError,
    refetch: () => {
      for (const read of reads) void read.refetch();
    },
  };
}
