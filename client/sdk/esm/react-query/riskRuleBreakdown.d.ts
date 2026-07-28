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
  GetRiskRuleBreakdownRequest,
  GetRiskRuleBreakdownSecurity,
} from "../models/operations/getriskrulebreakdown.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskRuleBreakdownQuery,
  prefetchRiskRuleBreakdown,
  queryKeyRiskRuleBreakdown,
  RiskRuleBreakdownQueryData,
} from "./riskRuleBreakdown.core.js";
export {
  buildRiskRuleBreakdownQuery,
  prefetchRiskRuleBreakdown,
  queryKeyRiskRuleBreakdown,
  type RiskRuleBreakdownQueryData,
};
export type RiskRuleBreakdownQueryError =
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
 * getRiskRuleBreakdown risk
 *
 * @remarks
 * Get per-rule_id finding counts for a category within a time window. Powers the per-category drill-down chart on /risk-overview.
 */
export declare function useRiskRuleBreakdown(
  request: GetRiskRuleBreakdownRequest,
  security?: GetRiskRuleBreakdownSecurity | undefined,
  options?: QueryHookOptions<
    RiskRuleBreakdownQueryData,
    RiskRuleBreakdownQueryError
  >,
): UseQueryResult<RiskRuleBreakdownQueryData, RiskRuleBreakdownQueryError>;
/**
 * getRiskRuleBreakdown risk
 *
 * @remarks
 * Get per-rule_id finding counts for a category within a time window. Powers the per-category drill-down chart on /risk-overview.
 */
export declare function useRiskRuleBreakdownSuspense(
  request: GetRiskRuleBreakdownRequest,
  security?: GetRiskRuleBreakdownSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskRuleBreakdownQueryData,
    RiskRuleBreakdownQueryError
  >,
): UseSuspenseQueryResult<
  RiskRuleBreakdownQueryData,
  RiskRuleBreakdownQueryError
>;
export declare function setRiskRuleBreakdownData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      category: string;
      from?: Date | undefined;
      to?: Date | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskRuleBreakdownQueryData,
): RiskRuleBreakdownQueryData | undefined;
export declare function invalidateRiskRuleBreakdown(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        category: string;
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
export declare function invalidateAllRiskRuleBreakdown(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskRuleBreakdown.d.ts.map
