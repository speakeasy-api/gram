import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAuditLogFacetsRequest, ListAuditLogFacetsSecurity } from "../models/operations/listauditlogfacets.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { AuditLogFacetsQueryData, buildAuditLogFacetsQuery, prefetchAuditLogFacets, queryKeyAuditLogFacets } from "./auditLogFacets.core.js";
export { type AuditLogFacetsQueryData, buildAuditLogFacetsQuery, prefetchAuditLogFacets, queryKeyAuditLogFacets, };
export type AuditLogFacetsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listFacets auditlogs
 *
 * @remarks
 * List available audit log facet values across organization and projects.
 */
export declare function useAuditLogFacets(request?: ListAuditLogFacetsRequest | undefined, security?: ListAuditLogFacetsSecurity | undefined, options?: QueryHookOptions<AuditLogFacetsQueryData, AuditLogFacetsQueryError>): UseQueryResult<AuditLogFacetsQueryData, AuditLogFacetsQueryError>;
/**
 * listFacets auditlogs
 *
 * @remarks
 * List available audit log facet values across organization and projects.
 */
export declare function useAuditLogFacetsSuspense(request?: ListAuditLogFacetsRequest | undefined, security?: ListAuditLogFacetsSecurity | undefined, options?: SuspenseQueryHookOptions<AuditLogFacetsQueryData, AuditLogFacetsQueryError>): UseSuspenseQueryResult<AuditLogFacetsQueryData, AuditLogFacetsQueryError>;
export declare function setAuditLogFacetsData(client: QueryClient, queryKeyBase: [
    parameters: {
        projectSlug?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
], data: AuditLogFacetsQueryData): AuditLogFacetsQueryData | undefined;
export declare function invalidateAuditLogFacets(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        projectSlug?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllAuditLogFacets(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=auditLogFacets.d.ts.map