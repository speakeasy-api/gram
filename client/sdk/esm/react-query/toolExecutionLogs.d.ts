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
  buildToolExecutionLogsQuery,
  prefetchToolExecutionLogs,
  queryKeyToolExecutionLogs,
  ToolExecutionLogsQueryData,
} from "./toolExecutionLogs.core.js";
export {
  buildToolExecutionLogsQuery,
  prefetchToolExecutionLogs,
  queryKeyToolExecutionLogs,
  type ToolExecutionLogsQueryData,
};
/**
 * listToolExecutionLogs logs
 *
 * @remarks
 * List structured logs from tool executions.
 */
export declare function useToolExecutionLogs(
  request?: operations.ListToolExecutionLogsRequest | undefined,
  security?: operations.ListToolExecutionLogsSecurity | undefined,
  options?: QueryHookOptions<ToolExecutionLogsQueryData>,
): UseQueryResult<ToolExecutionLogsQueryData, Error>;
/**
 * listToolExecutionLogs logs
 *
 * @remarks
 * List structured logs from tool executions.
 */
export declare function useToolExecutionLogsSuspense(
  request?: operations.ListToolExecutionLogsRequest | undefined,
  security?: operations.ListToolExecutionLogsSecurity | undefined,
  options?: SuspenseQueryHookOptions<ToolExecutionLogsQueryData>,
): UseSuspenseQueryResult<ToolExecutionLogsQueryData, Error>;
export declare function setToolExecutionLogsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      tsStart?: Date | undefined;
      tsEnd?: Date | undefined;
      deploymentId?: string | undefined;
      functionId?: string | undefined;
      instance?: string | undefined;
      level?: operations.Level | undefined;
      source?: operations.Source | undefined;
      cursor?: string | undefined;
      perPage?: number | undefined;
      direction?: operations.QueryParamDirection | undefined;
      sort?: operations.QueryParamSort | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: ToolExecutionLogsQueryData,
): ToolExecutionLogsQueryData | undefined;
export declare function invalidateToolExecutionLogs(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        tsStart?: Date | undefined;
        tsEnd?: Date | undefined;
        deploymentId?: string | undefined;
        functionId?: string | undefined;
        instance?: string | undefined;
        level?: operations.Level | undefined;
        source?: operations.Source | undefined;
        cursor?: string | undefined;
        perPage?: number | undefined;
        direction?: operations.QueryParamDirection | undefined;
        sort?: operations.QueryParamSort | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllToolExecutionLogs(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=toolExecutionLogs.d.ts.map
