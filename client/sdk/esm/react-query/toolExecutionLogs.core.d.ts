import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ToolExecutionLogsQueryData = components.ListToolExecutionLogsResult;
export declare function prefetchToolExecutionLogs(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.ListToolExecutionLogsRequest | undefined,
  security?: operations.ListToolExecutionLogsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildToolExecutionLogsQuery(
  client$: GramCore,
  request?: operations.ListToolExecutionLogsRequest | undefined,
  security?: operations.ListToolExecutionLogsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ToolExecutionLogsQueryData>;
};
export declare function queryKeyToolExecutionLogs(parameters: {
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
}): QueryKey;
//# sourceMappingURL=toolExecutionLogs.core.d.ts.map
