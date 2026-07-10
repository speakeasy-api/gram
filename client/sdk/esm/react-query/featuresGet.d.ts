import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { buildFeaturesGetQuery, FeaturesGetQueryData, prefetchFeaturesGet, queryKeyFeaturesGet } from "./featuresGet.core.js";
export { buildFeaturesGetQuery, type FeaturesGetQueryData, prefetchFeaturesGet, queryKeyFeaturesGet, };
export type FeaturesGetQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getProductFeatures features
 *
 * @remarks
 * Get the current state of all product feature flags.
 */
export declare function useFeaturesGet(request?: operations.GetProductFeaturesRequest | undefined, security?: operations.GetProductFeaturesSecurity | undefined, options?: QueryHookOptions<FeaturesGetQueryData, FeaturesGetQueryError>): UseQueryResult<FeaturesGetQueryData, FeaturesGetQueryError>;
/**
 * getProductFeatures features
 *
 * @remarks
 * Get the current state of all product feature flags.
 */
export declare function useFeaturesGetSuspense(request?: operations.GetProductFeaturesRequest | undefined, security?: operations.GetProductFeaturesSecurity | undefined, options?: SuspenseQueryHookOptions<FeaturesGetQueryData, FeaturesGetQueryError>): UseSuspenseQueryResult<FeaturesGetQueryData, FeaturesGetQueryError>;
export declare function setFeaturesGetData(client: QueryClient, queryKeyBase: [parameters: {
    gramSession?: string | undefined;
}], data: FeaturesGetQueryData): FeaturesGetQueryData | undefined;
export declare function invalidateFeaturesGet(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        gramSession?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllFeaturesGet(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=featuresGet.d.ts.map