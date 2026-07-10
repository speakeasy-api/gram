import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListCollectionsRequest, ListCollectionsSecurity } from "../models/operations/listcollections.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListCollectionsQuery, ListCollectionsQueryData, prefetchListCollections, queryKeyListCollections } from "./listCollections.core.js";
export { buildListCollectionsQuery, type ListCollectionsQueryData, prefetchListCollections, queryKeyListCollections, };
export type ListCollectionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * list collections
 *
 * @remarks
 * List MCP collections in the organization
 */
export declare function useListCollections(request?: ListCollectionsRequest | undefined, security?: ListCollectionsSecurity | undefined, options?: QueryHookOptions<ListCollectionsQueryData, ListCollectionsQueryError>): UseQueryResult<ListCollectionsQueryData, ListCollectionsQueryError>;
/**
 * list collections
 *
 * @remarks
 * List MCP collections in the organization
 */
export declare function useListCollectionsSuspense(request?: ListCollectionsRequest | undefined, security?: ListCollectionsSecurity | undefined, options?: SuspenseQueryHookOptions<ListCollectionsQueryData, ListCollectionsQueryError>): UseSuspenseQueryResult<ListCollectionsQueryData, ListCollectionsQueryError>;
export declare function setListCollectionsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
], data: ListCollectionsQueryData): ListCollectionsQueryData | undefined;
export declare function invalidateListCollections(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListCollections(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listCollections.d.ts.map