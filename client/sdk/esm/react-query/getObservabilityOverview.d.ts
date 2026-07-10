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
  GetObservabilityOverviewRequest,
  GetObservabilityOverviewSecurity,
} from "../models/operations/getobservabilityoverview.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildGetObservabilityOverviewQuery,
  GetObservabilityOverviewQueryData,
  prefetchGetObservabilityOverview,
  queryKeyGetObservabilityOverview,
} from "./getObservabilityOverview.core.js";
export {
  buildGetObservabilityOverviewQuery,
  type GetObservabilityOverviewQueryData,
  prefetchGetObservabilityOverview,
  queryKeyGetObservabilityOverview,
};
export type GetObservabilityOverviewQueryError =
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
 * getObservabilityOverview telemetry
 *
 * @remarks
 * Get observability overview metrics including time series, tool breakdowns, and summary stats
 */
export declare function useGetObservabilityOverview(
  request: GetObservabilityOverviewRequest,
  security?: GetObservabilityOverviewSecurity | undefined,
  options?: QueryHookOptions<
    GetObservabilityOverviewQueryData,
    GetObservabilityOverviewQueryError
  >,
): UseQueryResult<
  GetObservabilityOverviewQueryData,
  GetObservabilityOverviewQueryError
>;
/**
 * getObservabilityOverview telemetry
 *
 * @remarks
 * Get observability overview metrics including time series, tool breakdowns, and summary stats
 */
export declare function useGetObservabilityOverviewSuspense(
  request: GetObservabilityOverviewRequest,
  security?: GetObservabilityOverviewSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    GetObservabilityOverviewQueryData,
    GetObservabilityOverviewQueryError
  >,
): UseSuspenseQueryResult<
  GetObservabilityOverviewQueryData,
  GetObservabilityOverviewQueryError
>;
export declare function setGetObservabilityOverviewData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: GetObservabilityOverviewQueryData,
): GetObservabilityOverviewQueryData | undefined;
export declare function invalidateGetObservabilityOverview(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllGetObservabilityOverview(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=getObservabilityOverview.d.ts.map
