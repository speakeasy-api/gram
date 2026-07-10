import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetIntegrationResult } from "../models/components/getintegrationresult.js";
import { IntegrationsNumberGetRequest, IntegrationsNumberGetSecurity } from "../models/operations/integrationsnumberget.js";
export type IntegrationsIntegrationsNumberGetQueryData = GetIntegrationResult;
export declare function prefetchIntegrationsIntegrationsNumberGet(queryClient: QueryClient, client$: GramCore, request?: IntegrationsNumberGetRequest | undefined, security?: IntegrationsNumberGetSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildIntegrationsIntegrationsNumberGetQuery(client$: GramCore, request?: IntegrationsNumberGetRequest | undefined, security?: IntegrationsNumberGetSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<IntegrationsIntegrationsNumberGetQueryData>;
};
export declare function queryKeyIntegrationsIntegrationsNumberGet(parameters: {
    id?: string | undefined;
    name?: string | undefined;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=integrationsIntegrationsNumberGet.core.d.ts.map