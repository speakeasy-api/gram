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
  CreditUsageRequest,
  CreditUsageSecurity,
} from "../models/operations/creditusage.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetCreditUsageQuery,
  GetCreditUsageQueryData,
  prefetchGetCreditUsage,
  queryKeyGetCreditUsage,
} from "./getCreditUsage.core.js";
export {
  buildGetCreditUsageQuery,
  type GetCreditUsageQueryData,
  prefetchGetCreditUsage,
  queryKeyGetCreditUsage,
};
export type GetCreditUsageQueryError =
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
 * creditUsage chat
 *
 * @remarks
 * Get the total number of chat credits and usage for the current billing period
 */
export declare function useGetCreditUsage(
  request?: CreditUsageRequest | undefined,
  security?: CreditUsageSecurity | undefined,
  options?: QueryHookOptions<GetCreditUsageQueryData, GetCreditUsageQueryError>,
): UseQueryResult<GetCreditUsageQueryData, GetCreditUsageQueryError>;
/**
 * creditUsage chat
 *
 * @remarks
 * Get the total number of chat credits and usage for the current billing period
 */
export declare function useGetCreditUsageSuspense(
  request?: CreditUsageRequest | undefined,
  security?: CreditUsageSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetCreditUsageQueryData,
    GetCreditUsageQueryError
  >,
): UseSuspenseQueryResult<GetCreditUsageQueryData, GetCreditUsageQueryError>;
export declare function setGetCreditUsageData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramSession?: string | undefined;
    },
  ],
  data: GetCreditUsageQueryData,
): GetCreditUsageQueryData | undefined;
export declare function invalidateGetCreditUsage(
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
export declare function invalidateAllGetCreditUsage(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getCreditUsage.d.ts.map
