import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListAuditLogsRequest, ListAuditLogsResponse, ListAuditLogsSecurity } from "../models/operations/listauditlogs.js";
import { PageIterator } from "../types/operations.js";
export type AuditLogsQueryData = ListAuditLogsResponse;
export type AuditLogsInfiniteQueryData = PageIterator<ListAuditLogsResponse, {
    cursor: string;
}>;
export type AuditLogsPageParams = PageIterator<ListAuditLogsResponse, {
    cursor: string;
}>["~next"];
export declare function prefetchAuditLogs(queryClient: QueryClient, client$: GramCore, request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function prefetchAuditLogsInfinite(queryClient: QueryClient, client$: GramCore, request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildAuditLogsQuery(client$: GramCore, request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<AuditLogsQueryData>;
};
export declare function buildAuditLogsInfiniteQuery(client$: GramCore, request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext<QueryKey, AuditLogsPageParams>) => Promise<AuditLogsInfiniteQueryData>;
};
export declare function queryKeyAuditLogs(parameters: {
    cursor?: string | undefined;
    projectSlug?: string | undefined;
    actorId?: string | undefined;
    action?: string | undefined;
    subjectType?: string | undefined;
    subjectId?: string | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
export declare function queryKeyAuditLogsInfinite(parameters: {
    cursor?: string | undefined;
    projectSlug?: string | undefined;
    actorId?: string | undefined;
    action?: string | undefined;
    subjectType?: string | undefined;
    subjectId?: string | undefined;
    gramKey?: string | undefined;
    gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=auditLogs.core.d.ts.map