import type { GramCore } from "@gram/client/core.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import type { QueryHookOptions } from "@gram/client/react-query/_types.js";
import {
  buildListSetupTasksQuery,
  type ListSetupTasksQueryData,
  type ListSetupTasksQueryError,
} from "@gram/client/react-query/listSetupTasks.js";

export function buildOrganizationSetupTasksQuery(
  client: GramCore,
  organizationId: string,
  includeHidden: boolean,
  options?: QueryHookOptions<ListSetupTasksQueryData, ListSetupTasksQueryError>,
): ReturnType<typeof buildListSetupTasksQuery> &
  Pick<
    QueryHookOptions<ListSetupTasksQueryData, ListSetupTasksQueryError>,
    "throwOnError"
  > {
  const queryOptions = { throwOnError: false, ...options };
  const query = buildListSetupTasksQuery(
    client,
    { includeHidden, gramSession: "" },
    { sessionHeaderGramSession: "" },
    queryOptions,
  );

  return {
    ...query,
    ...queryOptions,
    queryKey: [...query.queryKey, { organizationId }],
  };
}

export function invalidateOrganizationSetupTasks(
  client: QueryClient,
  organizationId: string,
): Promise<void> {
  return client.invalidateQueries({
    queryKey: ["@gram/client", "organizations", "listSetupTasks"],
    predicate: (query) =>
      query.queryKey.some(
        (part) =>
          typeof part === "object" &&
          part !== null &&
          "organizationId" in part &&
          part.organizationId === organizationId,
      ),
  });
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
    throwOnError: false,
    ...options,
  });
}
