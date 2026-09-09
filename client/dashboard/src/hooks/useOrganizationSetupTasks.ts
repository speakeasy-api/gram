import type { GramCore } from "@gram/client/core.js";
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
