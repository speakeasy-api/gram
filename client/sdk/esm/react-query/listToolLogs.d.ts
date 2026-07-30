import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildListToolLogsQuery,
  ListToolLogsQueryData,
  prefetchListToolLogs,
  queryKeyListToolLogs,
} from "./listToolLogs.core.js";
export {
  buildListToolLogsQuery,
  type ListToolLogsQueryData,
  prefetchListToolLogs,
  queryKeyListToolLogs,
};
/**
 * listLogs logs
 *
 * @remarks
 * List call logs for a toolset.
 */
export declare function useListToolLogs(
  request?: operations.ListToolLogsRequest | undefined,
  security?: operations.ListToolLogsSecurity | undefined,
  options?: QueryHookOptions<ListToolLogsQueryData>,
): UseQueryResult<ListToolLogsQueryData, Error>;
/**
 * listLogs logs
 *
 * @remarks
 * List call logs for a toolset.
 */
export declare function useListToolLogsSuspense(
  request?: operations.ListToolLogsRequest | undefined,
  security?: operations.ListToolLogsSecurity | undefined,
  options?: SuspenseQueryHookOptions<ListToolLogsQueryData>,
): UseSuspenseQueryResult<ListToolLogsQueryData, Error>;
export declare function setListToolLogsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      toolId?: string | undefined;
      tsStart?: Date | undefined;
      tsEnd?: Date | undefined;
      cursor?: string | undefined;
      status?: operations.Status | undefined;
      serverName?: string | undefined;
      toolName?: string | undefined;
      toolType?: operations.ToolType | undefined;
      toolUrns?: Array<string> | undefined;
      perPage?: number | undefined;
      direction?: operations.Direction | undefined;
      sort?: operations.Sort | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ListToolLogsQueryData,
): ListToolLogsQueryData | undefined;
export declare function invalidateListToolLogs(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        toolId?: string | undefined;
        tsStart?: Date | undefined;
        tsEnd?: Date | undefined;
        cursor?: string | undefined;
        status?: operations.Status | undefined;
        serverName?: string | undefined;
        toolName?: string | undefined;
        toolType?: operations.ToolType | undefined;
        toolUrns?: Array<string> | undefined;
        perPage?: number | undefined;
        direction?: operations.Direction | undefined;
        sort?: operations.Sort | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllListToolLogs(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=listToolLogs.d.ts.map
