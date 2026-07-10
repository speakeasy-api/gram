import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListShadowMCPApprovalRequestsRequest, ListShadowMCPApprovalRequestsSecurity, Status } from "../models/operations/listshadowmcpapprovalrequests.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildShadowMCPApprovalRequestsQuery, prefetchShadowMCPApprovalRequests, queryKeyShadowMCPApprovalRequests, ShadowMCPApprovalRequestsQueryData } from "./shadowMCPApprovalRequests.core.js";
export { buildShadowMCPApprovalRequestsQuery, prefetchShadowMCPApprovalRequests, queryKeyShadowMCPApprovalRequests, type ShadowMCPApprovalRequestsQueryData, };
export type ShadowMCPApprovalRequestsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listShadowMCPApprovalRequests access
 *
 * @remarks
 * List Shadow MCP approval requests for the current organization. Requires organization admin access because requests include requester and block details.
 */
export declare function useShadowMCPApprovalRequests(request?: ListShadowMCPApprovalRequestsRequest | undefined, security?: ListShadowMCPApprovalRequestsSecurity | undefined, options?: QueryHookOptions<ShadowMCPApprovalRequestsQueryData, ShadowMCPApprovalRequestsQueryError>): UseQueryResult<ShadowMCPApprovalRequestsQueryData, ShadowMCPApprovalRequestsQueryError>;
/**
 * listShadowMCPApprovalRequests access
 *
 * @remarks
 * List Shadow MCP approval requests for the current organization. Requires organization admin access because requests include requester and block details.
 */
export declare function useShadowMCPApprovalRequestsSuspense(request?: ListShadowMCPApprovalRequestsRequest | undefined, security?: ListShadowMCPApprovalRequestsSecurity | undefined, options?: SuspenseQueryHookOptions<ShadowMCPApprovalRequestsQueryData, ShadowMCPApprovalRequestsQueryError>): UseSuspenseQueryResult<ShadowMCPApprovalRequestsQueryData, ShadowMCPApprovalRequestsQueryError>;
export declare function setShadowMCPApprovalRequestsData(client: QueryClient, queryKeyBase: [
    parameters: {
        status?: Status | undefined;
        projectId?: string | undefined;
        limit?: number | undefined;
        cursor?: string | undefined;
        gramSession?: string | undefined;
    }
], data: ShadowMCPApprovalRequestsQueryData): ShadowMCPApprovalRequestsQueryData | undefined;
export declare function invalidateShadowMCPApprovalRequests(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        status?: Status | undefined;
        projectId?: string | undefined;
        limit?: number | undefined;
        cursor?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllShadowMCPApprovalRequests(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=shadowMCPApprovalRequests.d.ts.map