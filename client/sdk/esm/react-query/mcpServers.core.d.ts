import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListMcpServersResult } from "../models/components/listmcpserversresult.js";
import {
  ListMcpServersRequest,
  ListMcpServersSecurity,
} from "../models/operations/listmcpservers.js";
export type McpServersQueryData = ListMcpServersResult;
export declare function prefetchMcpServers(
  queryClient: QueryClient,
  client$: GramCore,
  request?: ListMcpServersRequest | undefined,
  security?: ListMcpServersSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildMcpServersQuery(
  client$: GramCore,
  request?: ListMcpServersRequest | undefined,
  security?: ListMcpServersSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<McpServersQueryData>;
};
export declare function queryKeyMcpServers(parameters: {
  remoteMcpServerId?: string | undefined;
  tunneledMcpServerId?: string | undefined;
  toolsetId?: string | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=mcpServers.core.d.ts.map
