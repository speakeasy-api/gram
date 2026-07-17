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
  ListGlobalVariationsRequest,
  ListGlobalVariationsSecurity,
} from "../models/operations/listglobalvariations.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGlobalVariationsQuery,
  GlobalVariationsQueryData,
  prefetchGlobalVariations,
  queryKeyGlobalVariations,
} from "./globalVariations.core.js";
export {
  buildGlobalVariationsQuery,
  type GlobalVariationsQueryData,
  prefetchGlobalVariations,
  queryKeyGlobalVariations,
};
export type GlobalVariationsQueryError =
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
 * listGlobal variations
 *
 * @remarks
 * List globally defined tool variations.
 */
export declare function useGlobalVariations(
  request?: ListGlobalVariationsRequest | undefined,
  security?: ListGlobalVariationsSecurity | undefined,
  options?: QueryHookOptions<
    GlobalVariationsQueryData,
    GlobalVariationsQueryError
  >,
): UseQueryResult<GlobalVariationsQueryData, GlobalVariationsQueryError>;
/**
 * listGlobal variations
 *
 * @remarks
 * List globally defined tool variations.
 */
export declare function useGlobalVariationsSuspense(
  request?: ListGlobalVariationsRequest | undefined,
  security?: ListGlobalVariationsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GlobalVariationsQueryData,
    GlobalVariationsQueryError
  >,
): UseSuspenseQueryResult<
  GlobalVariationsQueryData,
  GlobalVariationsQueryError
>;
export declare function setGlobalVariationsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: GlobalVariationsQueryData,
): GlobalVariationsQueryData | undefined;
export declare function invalidateGlobalVariations(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramSession?: string | undefined;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGlobalVariations(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=globalVariations.d.ts.map
