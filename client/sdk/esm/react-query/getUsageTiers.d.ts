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
import { QueryHookOptions, SuspenseQueryHookOptions } from "./_types.js";
import {
  buildGetUsageTiersQuery,
  GetUsageTiersQueryData,
  prefetchGetUsageTiers,
  queryKeyGetUsageTiers,
} from "./getUsageTiers.core.js";
export {
  buildGetUsageTiersQuery,
  type GetUsageTiersQueryData,
  prefetchGetUsageTiers,
  queryKeyGetUsageTiers,
};
export type GetUsageTiersQueryError =
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
 * getUsageTiers usage
 *
 * @remarks
 * Get the usage tiers
 */
export declare function useGetUsageTiers(
  options?: QueryHookOptions<GetUsageTiersQueryData, GetUsageTiersQueryError>,
): UseQueryResult<GetUsageTiersQueryData, GetUsageTiersQueryError>;
/**
 * getUsageTiers usage
 *
 * @remarks
 * Get the usage tiers
 */
export declare function useGetUsageTiersSuspense(
  options?: SuspenseQueryHookOptions<
    GetUsageTiersQueryData,
    GetUsageTiersQueryError
  >,
): UseSuspenseQueryResult<GetUsageTiersQueryData, GetUsageTiersQueryError>;
export declare function setGetUsageTiersData(
  client: QueryClient,
  data: GetUsageTiersQueryData,
): GetUsageTiersQueryData | undefined;
export declare function invalidateAllGetUsageTiers(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getUsageTiers.d.ts.map
