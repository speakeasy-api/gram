import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpServer } from "../models/components/mcpserver.js";
import {
  GetMcpServerRequest,
  GetMcpServerSecurity,
} from "../models/operations/getmcpserver.js";
export type GetMcpServerQueryData = McpServer;
export declare function prefetchGetMcpServer(
  queryClient: QueryClient,
  client$: GramCore,
  request?: GetMcpServerRequest | undefined,
  security?: GetMcpServerSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetMcpServerQuery(
  client$: GramCore,
  request?: GetMcpServerRequest | undefined,
  security?: GetMcpServerSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GetMcpServerQueryData>;
};
export declare function queryKeyGetMcpServer(parameters: {
  id?: string | undefined;
  slug?: string | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getMcpServer.core.d.ts.map
