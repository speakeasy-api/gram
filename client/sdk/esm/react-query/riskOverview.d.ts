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
  GetRiskOverviewRequest,
  GetRiskOverviewSecurity,
} from "../models/operations/getriskoverview.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskOverviewQuery,
  prefetchRiskOverview,
  queryKeyRiskOverview,
  RiskOverviewQueryData,
} from "./riskOverview.core.js";
export {
  buildRiskOverviewQuery,
  prefetchRiskOverview,
  queryKeyRiskOverview,
  type RiskOverviewQueryData,
};
export type RiskOverviewQueryError =
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
 * getRiskOverview risk
 *
 * @remarks
 * Get risk overview metrics and trend data for the current project.
 */
export declare function useRiskOverview(
  request?: GetRiskOverviewRequest | undefined,
  security?: GetRiskOverviewSecurity | undefined,
  options?: QueryHookOptions<RiskOverviewQueryData, RiskOverviewQueryError>,
): UseQueryResult<RiskOverviewQueryData, RiskOverviewQueryError>;
/**
 * getRiskOverview risk
 *
 * @remarks
 * Get risk overview metrics and trend data for the current project.
 */
export declare function useRiskOverviewSuspense(
  request?: GetRiskOverviewRequest | undefined,
  security?: GetRiskOverviewSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskOverviewQueryData,
    RiskOverviewQueryError
  >,
): UseSuspenseQueryResult<RiskOverviewQueryData, RiskOverviewQueryError>;
export declare function setRiskOverviewData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      from?: Date | undefined;
      to?: Date | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskOverviewQueryData,
): RiskOverviewQueryData | undefined;
export declare function invalidateRiskOverview(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        from?: Date | undefined;
        to?: Date | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskOverview(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskOverview.d.ts.map
