import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListScopesRequest, ListScopesSecurity } from "../models/operations/listscopes.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListScopesQuery, ListScopesQueryData, prefetchListScopes, queryKeyListScopes } from "./listScopes.core.js";
export { buildListScopesQuery, type ListScopesQueryData, prefetchListScopes, queryKeyListScopes, };
export type ListScopesQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listScopes access
 *
 * @remarks
 * List all available scopes and their resource types.
 */
export declare function useListScopes(request?: ListScopesRequest | undefined, security?: ListScopesSecurity | undefined, options?: QueryHookOptions<ListScopesQueryData, ListScopesQueryError>): UseQueryResult<ListScopesQueryData, ListScopesQueryError>;
/**
 * listScopes access
 *
 * @remarks
 * List all available scopes and their resource types.
 */
export declare function useListScopesSuspense(request?: ListScopesRequest | undefined, security?: ListScopesSecurity | undefined, options?: SuspenseQueryHookOptions<ListScopesQueryData, ListScopesQueryError>): UseSuspenseQueryResult<ListScopesQueryData, ListScopesQueryError>;
export declare function setListScopesData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
], data: ListScopesQueryData): ListScopesQueryData | undefined;
export declare function invalidateListScopes(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListScopes(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listScopes.d.ts.map