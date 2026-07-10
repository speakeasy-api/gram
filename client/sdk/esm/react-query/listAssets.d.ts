import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListAssetsRequest, ListAssetsSecurity } from "../models/operations/listassets.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildListAssetsQuery, ListAssetsQueryData, prefetchListAssets, queryKeyListAssets } from "./listAssets.core.js";
export { buildListAssetsQuery, type ListAssetsQueryData, prefetchListAssets, queryKeyListAssets, };
export type ListAssetsQueryError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * listAssets assets
 *
 * @remarks
 * List all assets for a project.
 */
export declare function useListAssets(request?: ListAssetsRequest | undefined, security?: ListAssetsSecurity | undefined, options?: QueryHookOptions<ListAssetsQueryData, ListAssetsQueryError>): UseQueryResult<ListAssetsQueryData, ListAssetsQueryError>;
/**
 * listAssets assets
 *
 * @remarks
 * List all assets for a project.
 */
export declare function useListAssetsSuspense(request?: ListAssetsRequest | undefined, security?: ListAssetsSecurity | undefined, options?: SuspenseQueryHookOptions<ListAssetsQueryData, ListAssetsQueryError>): UseSuspenseQueryResult<ListAssetsQueryData, ListAssetsQueryError>;
export declare function setListAssetsData(client: QueryClient, queryKeyBase: [
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
        gramKey?: string | undefined;
    }
], data: ListAssetsQueryData): ListAssetsQueryData | undefined;
export declare function invalidateListAssets(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
        gramProject?: string | undefined;
        gramKey?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllListAssets(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=listAssets.d.ts.map