import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListIntegrationsResult } from "../models/components/listintegrationsresult.js";
import { ListIntegrationsRequest, ListIntegrationsSecurity } from "../models/operations/listintegrations.js";
export type ListIntegrationsQueryData = ListIntegrationsResult;
export declare function prefetchListIntegrations(queryClient: QueryClient, client$: GramCore, request?: ListIntegrationsRequest | undefined, security?: ListIntegrationsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildListIntegrationsQuery(client$: GramCore, request?: ListIntegrationsRequest | undefined, security?: ListIntegrationsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<ListIntegrationsQueryData>;
};
export declare function queryKeyListIntegrations(parameters: {
    keywords?: Array<string> | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=listIntegrations.core.d.ts.map