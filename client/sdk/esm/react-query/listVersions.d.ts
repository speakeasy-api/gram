import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListVersionsRequest, ListVersionsSecurity } from "../models/operations/listversions.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListVersionsQuery, ListVersionsQueryData, prefetchListVersions, queryKeyListVersions } from "./listVersions.core.js";
export { buildListVersionsQuery, type ListVersionsQueryData, prefetchListVersions, queryKeyListVersions, };
export type ListVersionsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listVersions packages
 *
 * @remarks
 * List published versions of a package.
 */
export declare function useListVersions(request: ListVersionsRequest, security?: ListVersionsSecurity | undefined, options?: QueryHookOptions<ListVersionsQueryData, ListVersionsQueryError>): UseQueryResult<ListVersionsQueryData, ListVersionsQueryError>;
/**
 * listVersions packages
 *
 * @remarks
 * List published versions of a package.
 */
export declare function useListVersionsSuspense(request: ListVersionsRequest, security?: ListVersionsSecurity | undefined, options?: SuspenseQueryHookOptions<ListVersionsQueryData, ListVersionsQueryError>): UseSuspenseQueryResult<ListVersionsQueryData, ListVersionsQueryError>;
export declare function setListVersionsData(client: QueryClient, queryKeyBase: [
    parameters: {
        name: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
], data: ListVersionsQueryData): ListVersionsQueryData | undefined;
export declare function invalidateListVersions(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        name: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListVersions(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listVersions.d.ts.map