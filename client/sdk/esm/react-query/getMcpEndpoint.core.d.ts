import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { McpEndpoint } from "../models/components/mcpendpoint.js";
import {
  GetMcpEndpointRequest,
  GetMcpEndpointSecurity,
} from "../models/operations/getmcpendpoint.js";
export type GetMcpEndpointQueryData = McpEndpoint;
export declare function prefetchGetMcpEndpoint(
  queryClient: QueryClient,
  client$: GramCore,
  request?: GetMcpEndpointRequest | undefined,
  security?: GetMcpEndpointSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetMcpEndpointQuery(
  client$: GramCore,
  request?: GetMcpEndpointRequest | undefined,
  security?: GetMcpEndpointSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<GetMcpEndpointQueryData>;
};
export declare function queryKeyGetMcpEndpoint(parameters: {
  id?: string | undefined;
  customDomainId?: string | undefined;
  slug?: string | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getMcpEndpoint.core.d.ts.map
