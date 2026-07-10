import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListCustomDomainMcpEndpointsResult } from "../models/components/listcustomdomainmcpendpointsresult.js";
import { ListCustomDomainMcpEndpointsRequest, ListCustomDomainMcpEndpointsSecurity } from "../models/operations/listcustomdomainmcpendpoints.js";
export type CustomDomainMcpEndpointsQueryData = ListCustomDomainMcpEndpointsResult;
export declare function prefetchCustomDomainMcpEndpoints(queryClient: QueryClient, client$: GramCore, request?: ListCustomDomainMcpEndpointsRequest | undefined, security?: ListCustomDomainMcpEndpointsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildCustomDomainMcpEndpointsQuery(client$: GramCore, request?: ListCustomDomainMcpEndpointsRequest | undefined, security?: ListCustomDomainMcpEndpointsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<CustomDomainMcpEndpointsQueryData>;
};
export declare function queryKeyCustomDomainMcpEndpoints(parameters: {
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=customDomainMcpEndpoints.core.d.ts.map