import { InfiniteData, InvalidateQueryFilters, QueryClient, UseInfiniteQueryResult, UseQueryResult, UseSuspenseInfiniteQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAssistantMemoriesRequest, ListAssistantMemoriesSecurity } from "../models/operations/listassistantmemories.js";
import { InfiniteQueryHookOptions, QueryHookOptions, SuspenseInfiniteQueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListAssistantMemoriesInfiniteQuery, buildListAssistantMemoriesQuery, ListAssistantMemoriesInfiniteQueryData, ListAssistantMemoriesPageParams, ListAssistantMemoriesQueryData, prefetchListAssistantMemories, prefetchListAssistantMemoriesInfinite, queryKeyListAssistantMemories, queryKeyListAssistantMemoriesInfinite } from "./listAssistantMemories.core.js";
export { buildListAssistantMemoriesInfiniteQuery, buildListAssistantMemoriesQuery, type ListAssistantMemoriesInfiniteQueryData, type ListAssistantMemoriesPageParams, type ListAssistantMemoriesQueryData, prefetchListAssistantMemories, prefetchListAssistantMemoriesInfinite, queryKeyListAssistantMemories, queryKeyListAssistantMemoriesInfinite, };
export type ListAssistantMemoriesQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listAssistantMemories assistantMemories
 *
 * @remarks
 * List assistant memories for an assistant.
 */
export declare function useListAssistantMemories(request: ListAssistantMemoriesRequest, security?: ListAssistantMemoriesSecurity | undefined, options?: QueryHookOptions<ListAssistantMemoriesQueryData, ListAssistantMemoriesQueryError>): UseQueryResult<ListAssistantMemoriesQueryData, ListAssistantMemoriesQueryError>;
/**
 * listAssistantMemories assistantMemories
 *
 * @remarks
 * List assistant memories for an assistant.
 */
export declare function useListAssistantMemoriesSuspense(request: ListAssistantMemoriesRequest, security?: ListAssistantMemoriesSecurity | undefined, options?: SuspenseQueryHookOptions<ListAssistantMemoriesQueryData, ListAssistantMemoriesQueryError>): UseSuspenseQueryResult<ListAssistantMemoriesQueryData, ListAssistantMemoriesQueryError>;
/**
 * listAssistantMemories assistantMemories
 *
 * @remarks
 * List assistant memories for an assistant.
 */
export declare function useListAssistantMemoriesInfinite(request: ListAssistantMemoriesRequest, security?: ListAssistantMemoriesSecurity | undefined, options?: InfiniteQueryHookOptions<ListAssistantMemoriesInfiniteQueryData, ListAssistantMemoriesQueryError>): UseInfiniteQueryResult<InfiniteData<ListAssistantMemoriesInfiniteQueryData, ListAssistantMemoriesPageParams>, ListAssistantMemoriesQueryError>;
/**
 * listAssistantMemories assistantMemories
 *
 * @remarks
 * List assistant memories for an assistant.
 */
export declare function useListAssistantMemoriesInfiniteSuspense(request: ListAssistantMemoriesRequest, security?: ListAssistantMemoriesSecurity | undefined, options?: SuspenseInfiniteQueryHookOptions<ListAssistantMemoriesInfiniteQueryData, ListAssistantMemoriesQueryError>): UseSuspenseInfiniteQueryResult<InfiniteData<ListAssistantMemoriesInfiniteQueryData, ListAssistantMemoriesPageParams>, ListAssistantMemoriesQueryError>;
export declare function setListAssistantMemoriesData(client: QueryClient, queryKeyBase: [
    parameters: {
        assistantId: string;
        tags?: Array<string> | undefined;
        includeDeleted?: boolean | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListAssistantMemoriesQueryData): ListAssistantMemoriesQueryData | undefined;
export declare function invalidateListAssistantMemories(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        assistantId: string;
        tags?: Array<string> | undefined;
        includeDeleted?: boolean | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListAssistantMemories(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listAssistantMemories.d.ts.map