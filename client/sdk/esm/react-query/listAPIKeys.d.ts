import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAPIKeysRequest, ListAPIKeysSecurity } from "../models/operations/listapikeys.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListAPIKeysQuery, ListAPIKeysQueryData, prefetchListAPIKeys, queryKeyListAPIKeys } from "./listAPIKeys.core.js";
export { buildListAPIKeysQuery, type ListAPIKeysQueryData, prefetchListAPIKeys, queryKeyListAPIKeys, };
export type ListAPIKeysQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listKeys keys
 *
 * @remarks
 * List all api keys for an organization
 */
export declare function useListAPIKeys(request?: ListAPIKeysRequest | undefined, security?: ListAPIKeysSecurity | undefined, options?: QueryHookOptions<ListAPIKeysQueryData, ListAPIKeysQueryError>): UseQueryResult<ListAPIKeysQueryData, ListAPIKeysQueryError>;
/**
 * listKeys keys
 *
 * @remarks
 * List all api keys for an organization
 */
export declare function useListAPIKeysSuspense(request?: ListAPIKeysRequest | undefined, security?: ListAPIKeysSecurity | undefined, options?: SuspenseQueryHookOptions<ListAPIKeysQueryData, ListAPIKeysQueryError>): UseSuspenseQueryResult<ListAPIKeysQueryData, ListAPIKeysQueryError>;
export declare function setListAPIKeysData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: ListAPIKeysQueryData): ListAPIKeysQueryData | undefined;
export declare function invalidateListAPIKeys(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListAPIKeys(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listAPIKeys.d.ts.map