import { RequestOptions } from "../lib/sdks.js";
import { PageIterator } from "../types/operations.js";
import type { DefaultError, InfiniteData, InfiniteQueryPageParamsOptions, OmitKeyof, QueryKey, QueryObserverOptions, SkipToken, UseMutationOptions, UseQueryOptions, UseSuspenseQueryOptions } from "@tanstack/react-query";
interface UseInfiniteQueryOptions<TQueryFnData = unknown, TError = DefaultError, TData = TQueryFnData, TQueryKey extends QueryKey = QueryKey, TPageParam = unknown> extends OmitKeyof<InfiniteQueryObserverOptions<TQueryFnData, TError, TData, TQueryKey, TPageParam>, "suspense"> {
    /**
     * Set this to `false` to unsubscribe this observer from updates to the query cache.
     * Defaults to `true`.
     */
    subscribed?: boolean;
}
interface InfiniteQueryObserverOptions<TQueryFnData = unknown, TError = DefaultError, TData = TQueryFnData, TQueryKey extends QueryKey = QueryKey, TPageParam = unknown> extends QueryObserverOptions<TQueryFnData, TError, TData, InfiniteData<TQueryFnData, TPageParam>, TQueryKey, TPageParam>, InfiniteQueryPageParamsOptions<TQueryFnData, TPageParam> {
}
interface UseSuspenseInfiniteQueryOptions<TQueryFnData = unknown, TError = DefaultError, TData = TQueryFnData, TQueryKey extends QueryKey = QueryKey, TPageParam = unknown> extends OmitKeyof<UseInfiniteQueryOptions<TQueryFnData, TError, TData, TQueryKey, TPageParam>, "queryFn" | "enabled" | "throwOnError" | "placeholderData"> {
    queryFn?: Exclude<UseInfiniteQueryOptions<TQueryFnData, TError, TData, TQueryKey, TPageParam>["queryFn"], SkipToken>;
}
export type TupleToPrefixes<T extends any[]> = T extends [...infer Prefix, any] ? TupleToPrefixes<Prefix> | T : never;
export type QueryHookOptions<Data, Err = Error> = Omit<UseQueryOptions<Data, Err>, "queryKey" | "queryFn" | "select" | keyof RequestOptions> & RequestOptions;
export type SuspenseQueryHookOptions<Data, Err = Error> = Omit<UseSuspenseQueryOptions<Data, Err>, "queryKey" | "queryFn" | "select" | keyof RequestOptions> & RequestOptions;
export type InfiniteQueryHookOptions<Data extends PageIterator<unknown, unknown>, Err = Error> = Omit<UseInfiniteQueryOptions<Data, Err, InfiniteData<Data, Data["~next"]>, QueryKey, Data["~next"]>, "queryKey" | "queryFn" | "select" | "getNextPageParam" | "getPreviousPageParam" | "initialPageParam" | keyof RequestOptions> & RequestOptions & {
    initialPageParam?: Data["~next"];
};
export type SuspenseInfiniteQueryHookOptions<Data extends PageIterator<unknown, unknown>, Err = Error> = Omit<UseSuspenseInfiniteQueryOptions<Data, Err, InfiniteData<Data, Data["~next"]>, QueryKey, Data["~next"]>, "queryKey" | "queryFn" | "select" | "getNextPageParam" | "getPreviousPageParam" | "initialPageParam" | keyof RequestOptions> & RequestOptions & {
    initialPageParam?: Data["~next"];
};
export type MutationHookOptions<Data = unknown, Err = Error, Variables = unknown> = Omit<UseMutationOptions<Data, Err, Variables>, "mutationKey" | "mutationFn" | keyof RequestOptions> & RequestOptions;
/**
 * Removes non-serializable properties (functions and symbols) from a PageIterator for SSR hydration.
 * React Server Components cannot serialize functions or Symbol properties across the server/client boundary.
 */
export declare function pageIteratorToJSON<T extends {
    "~next"?: unknown;
}>(page: T): T;
export {};
//# sourceMappingURL=_types.d.ts.map