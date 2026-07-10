import { InfiniteData, InvalidateQueryFilters, QueryClient, UseInfiniteQueryResult, UseQueryResult, UseSuspenseInfiniteQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAuditLogsRequest, ListAuditLogsSecurity } from "../models/operations/listauditlogs.js";
import { InfiniteQueryHookOptions, QueryHookOptions, SuspenseInfiniteQueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { AuditLogsInfiniteQueryData, AuditLogsPageParams, AuditLogsQueryData, buildAuditLogsInfiniteQuery, buildAuditLogsQuery, prefetchAuditLogs, prefetchAuditLogsInfinite, queryKeyAuditLogs, queryKeyAuditLogsInfinite } from "./auditLogs.core.js";
export { type AuditLogsInfiniteQueryData, type AuditLogsPageParams, type AuditLogsQueryData, buildAuditLogsInfiniteQuery, buildAuditLogsQuery, prefetchAuditLogs, prefetchAuditLogsInfinite, queryKeyAuditLogs, queryKeyAuditLogsInfinite, };
export type AuditLogsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * list auditlogs
 *
 * @remarks
 * List audit logs across organization and projects.
 */
export declare function useAuditLogs(request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: QueryHookOptions<AuditLogsQueryData, AuditLogsQueryError>): UseQueryResult<AuditLogsQueryData, AuditLogsQueryError>;
/**
 * list auditlogs
 *
 * @remarks
 * List audit logs across organization and projects.
 */
export declare function useAuditLogsSuspense(request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: SuspenseQueryHookOptions<AuditLogsQueryData, AuditLogsQueryError>): UseSuspenseQueryResult<AuditLogsQueryData, AuditLogsQueryError>;
/**
 * list auditlogs
 *
 * @remarks
 * List audit logs across organization and projects.
 */
export declare function useAuditLogsInfinite(request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: InfiniteQueryHookOptions<AuditLogsInfiniteQueryData, AuditLogsQueryError>): UseInfiniteQueryResult<InfiniteData<AuditLogsInfiniteQueryData, AuditLogsPageParams>, AuditLogsQueryError>;
/**
 * list auditlogs
 *
 * @remarks
 * List audit logs across organization and projects.
 */
export declare function useAuditLogsInfiniteSuspense(request?: ListAuditLogsRequest | undefined, security?: ListAuditLogsSecurity | undefined, options?: SuspenseInfiniteQueryHookOptions<AuditLogsInfiniteQueryData, AuditLogsQueryError>): UseSuspenseInfiniteQueryResult<InfiniteData<AuditLogsInfiniteQueryData, AuditLogsPageParams>, AuditLogsQueryError>;
export declare function setAuditLogsData(client: QueryClient, queryKeyBase: [
    parameters: {
        cursor?: string | undefined;
        projectSlug?: string | undefined;
        actorId?: string | undefined;
        action?: string | undefined;
        subjectType?: string | undefined;
        subjectId?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
], data: AuditLogsQueryData): AuditLogsQueryData | undefined;
export declare function invalidateAuditLogs(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        cursor?: string | undefined;
        projectSlug?: string | undefined;
        actorId?: string | undefined;
        action?: string | undefined;
        subjectType?: string | undefined;
        subjectId?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllAuditLogs(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=auditLogs.d.ts.map