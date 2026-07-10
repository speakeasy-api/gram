import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListOrganizationUsersRequest, ListOrganizationUsersSecurity } from "../models/operations/listorganizationusers.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListOrganizationUsersQuery, ListOrganizationUsersQueryData, prefetchListOrganizationUsers, queryKeyListOrganizationUsers } from "./listOrganizationUsers.core.js";
export { buildListOrganizationUsersQuery, type ListOrganizationUsersQueryData, prefetchListOrganizationUsers, queryKeyListOrganizationUsers, };
export type ListOrganizationUsersQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listUsers organizations
 *
 * @remarks
 * List users in the active organization from Gram organization_user_relationships.
 */
export declare function useListOrganizationUsers(request?: ListOrganizationUsersRequest | undefined, security?: ListOrganizationUsersSecurity | undefined, options?: QueryHookOptions<ListOrganizationUsersQueryData, ListOrganizationUsersQueryError>): UseQueryResult<ListOrganizationUsersQueryData, ListOrganizationUsersQueryError>;
/**
 * listUsers organizations
 *
 * @remarks
 * List users in the active organization from Gram organization_user_relationships.
 */
export declare function useListOrganizationUsersSuspense(request?: ListOrganizationUsersRequest | undefined, security?: ListOrganizationUsersSecurity | undefined, options?: SuspenseQueryHookOptions<ListOrganizationUsersQueryData, ListOrganizationUsersQueryError>): UseSuspenseQueryResult<ListOrganizationUsersQueryData, ListOrganizationUsersQueryError>;
export declare function setListOrganizationUsersData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: ListOrganizationUsersQueryData): ListOrganizationUsersQueryData | undefined;
export declare function invalidateListOrganizationUsers(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListOrganizationUsers(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listOrganizationUsers.d.ts.map