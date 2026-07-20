import {
  InvalidateQueryFilters,
  QueryClient,
  UseQueryResult,
  UseSuspenseQueryResult,
} from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import {
  ConnectionError,
  InvalidRequestError,
  RequestAbortedError,
  RequestTimeoutError,
  UnexpectedClientError,
} from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  GetProductFeaturesRequest,
  GetProductFeaturesSecurity,
} from "../models/operations/getproductfeatures.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildProductFeaturesQuery,
  prefetchProductFeatures,
  ProductFeaturesQueryData,
  queryKeyProductFeatures,
} from "./productFeatures.core.js";
export {
  buildProductFeaturesQuery,
  prefetchProductFeatures,
  type ProductFeaturesQueryData,
  queryKeyProductFeatures,
};
export type ProductFeaturesQueryError =
  | ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * getProductFeatures features
 *
 * @remarks
 * Get the current state of all product feature flags.
 */
export declare function useProductFeatures(
  request?: GetProductFeaturesRequest | undefined,
  security?: GetProductFeaturesSecurity | undefined,
  options?: QueryHookOptions<
    ProductFeaturesQueryData,
    ProductFeaturesQueryError
  >,
): UseQueryResult<ProductFeaturesQueryData, ProductFeaturesQueryError>;
/**
 * getProductFeatures features
 *
 * @remarks
 * Get the current state of all product feature flags.
 */
export declare function useProductFeaturesSuspense(
  request?: GetProductFeaturesRequest | undefined,
  security?: GetProductFeaturesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    ProductFeaturesQueryData,
    ProductFeaturesQueryError
  >,
): UseSuspenseQueryResult<ProductFeaturesQueryData, ProductFeaturesQueryError>;
export declare function setProductFeaturesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: ProductFeaturesQueryData,
): ProductFeaturesQueryData | undefined;
export declare function invalidateProductFeatures(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllProductFeatures(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=productFeatures.d.ts.map
