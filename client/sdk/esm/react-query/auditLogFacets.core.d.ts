import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAuditLogFacetsResult } from "../models/components/listauditlogfacetsresult.js";
import { ListAuditLogFacetsRequest, ListAuditLogFacetsSecurity } from "../models/operations/listauditlogfacets.js";
export type AuditLogFacetsQueryData = ListAuditLogFacetsResult;
export declare function prefetchAuditLogFacets(queryClient: QueryClient, client$: GramCore, request?: ListAuditLogFacetsRequest | undefined, security?: ListAuditLogFacetsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildAuditLogFacetsQuery(client$: GramCore, request?: ListAuditLogFacetsRequest | undefined, security?: ListAuditLogFacetsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<AuditLogFacetsQueryData>;
};
export declare function queryKeyAuditLogFacets(parameters: {
    projectSlug?: string | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=auditLogFacets.core.d.ts.map