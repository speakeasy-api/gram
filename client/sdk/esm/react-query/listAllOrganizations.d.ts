import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListAllOrganizationsQuery, ListAllOrganizationsQueryData, prefetchListAllOrganizations, queryKeyListAllOrganizations } from "./listAllOrganizations.core.js";
export { buildListAllOrganizationsQuery, type ListAllOrganizationsQueryData, prefetchListAllOrganizations, queryKeyListAllOrganizations, };
export type ListAllOrganizationsQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listAll organizations
 *
 * @remarks
 * List every Gram organization (admin only - requires speakeasy-team API key).
 */
export declare function useListAllOrganizations(request?: operations.ListAllOrganizationsRequest | undefined, security?: operations.ListAllOrganizationsSecurity | undefined, options?: QueryHookOptions<ListAllOrganizationsQueryData, ListAllOrganizationsQueryError>): UseQueryResult<ListAllOrganizationsQueryData, ListAllOrganizationsQueryError>;
/**
 * listAll organizations
 *
 * @remarks
 * List every Gram organization (admin only - requires speakeasy-team API key).
 */
export declare function useListAllOrganizationsSuspense(request?: operations.ListAllOrganizationsRequest | undefined, security?: operations.ListAllOrganizationsSecurity | undefined, options?: SuspenseQueryHookOptions<ListAllOrganizationsQueryData, ListAllOrganizationsQueryError>): UseSuspenseQueryResult<ListAllOrganizationsQueryData, ListAllOrganizationsQueryError>;
export declare function setListAllOrganizationsData(client: QueryClient, queryKeyBase: [
    parameters: {
        limit?: number | undefined;
        offset?: number | undefined;
        gramKey?: string | undefined;
    }
], data: ListAllOrganizationsQueryData): ListAllOrganizationsQueryData | undefined;
export declare function invalidateListAllOrganizations(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        limit?: number | undefined;
        offset?: number | undefined;
        gramKey?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListAllOrganizations(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listAllOrganizations.d.ts.map