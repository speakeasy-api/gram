import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export type ListToolLogsQueryData = components.ListToolLogResponse;
export declare function prefetchListToolLogs(
  queryClient: QueryClient,
  client$: GramCore,
  request?: operations.ListToolLogsRequest | undefined,
  security?: operations.ListToolLogsSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildListToolLogsQuery(
  client$: GramCore,
  request?: operations.ListToolLogsRequest | undefined,
  security?: operations.ListToolLogsSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ListToolLogsQueryData>;
};
export declare function queryKeyListToolLogs(parameters: {
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
}): QueryKey;
//# sourceMappingURL=listToolLogs.core.d.ts.map
