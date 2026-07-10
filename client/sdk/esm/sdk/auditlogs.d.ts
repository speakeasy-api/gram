import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListAuditLogFacetsResult } from "../models/components/listauditlogfacetsresult.js";
import { ListAuditLogFacetsRequest, ListAuditLogFacetsSecurity } from "../models/operations/listauditlogfacets.js";
import { ListAuditLogsRequest, ListAuditLogsResponse, ListAuditLogsSecurity } from "../models/operations/listauditlogs.js";
import { PageIterator } from "../types/operations.js";
export declare class Auditlogs extends ClientSDK {
    /**
     * list auditlogs
     *
     * @remarks
     * List audit logs across organization and projects.
     */
    list(request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: RequestOptions): Promise<PageIterator<ListAuditLogsResponse, {
        cursor: string;
    }>>;
    /**
     * listFacets auditlogs
     *
     * @remarks
     * List available audit log facet values across organization and projects.
     */
    listFacets(request?: ListAuditLogFacetsRequest | undefined, security?: ListAuditLogFacetsSecurity | undefined, options?: RequestOptions): Promise<ListAuditLogFacetsResult>;
}
//# sourceMappingURL=auditlogs.d.ts.map