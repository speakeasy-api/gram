import type { GramCore } from "@gram/client/core.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import type { QueryHookOptions } from "@gram/client/react-query/_types.js";
import {
  buildListSetupTasksQuery,
  type ListSetupTasksQueryData,
  type ListSetupTasksQueryError,
} from "@gram/client/react-query/listSetupTasks.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

export function buildOrganizationSetupTasksQuery(
  client: GramCore,
  organizationId: string,
  includeHidden: boolean,
  options?: QueryHookOptions<ListSetupTasksQueryData, ListSetupTasksQueryError>,
): ReturnType<typeof buildListSetupTasksQuery> {
  const query = buildListSetupTasksQuery(
    client,
    { includeHidden, gramSession: "" },
    { sessionHeaderGramSession: "" },
    options,
  );

  return {
    ...query,
    queryKey: [...query.queryKey, { organizationId }],
  };
}

export function useOrganizationSetupTasks(
  organizationId: string,
  includeHidden: boolean,
  options?: QueryHookOptions<ListSetupTasksQueryData, ListSetupTasksQueryError>,
): UseQueryResult<ListSetupTasksQueryData, ListSetupTasksQueryError> {
  const client = useGramContext();
  return useQuery({
    ...buildOrganizationSetupTasksQuery(
      client,
      organizationId,
      includeHidden,
      options,
    ),
    ...options,
  });
}
