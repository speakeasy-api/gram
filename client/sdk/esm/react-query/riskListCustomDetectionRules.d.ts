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
  ListCustomDetectionRulesRequest,
  ListCustomDetectionRulesSecurity,
} from "../models/operations/listcustomdetectionrules.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskListCustomDetectionRulesQuery,
  prefetchRiskListCustomDetectionRules,
  queryKeyRiskListCustomDetectionRules,
  RiskListCustomDetectionRulesQueryData,
} from "./riskListCustomDetectionRules.core.js";
export {
  buildRiskListCustomDetectionRulesQuery,
  prefetchRiskListCustomDetectionRules,
  queryKeyRiskListCustomDetectionRules,
  type RiskListCustomDetectionRulesQueryData,
};
export type RiskListCustomDetectionRulesQueryError =
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
 * listCustomDetectionRules risk
 *
 * @remarks
 * List custom detection rules for the current project.
 */
export declare function useRiskListCustomDetectionRules(
  request?: ListCustomDetectionRulesRequest | undefined,
  security?: ListCustomDetectionRulesSecurity | undefined,
  options?: QueryHookOptions<
    RiskListCustomDetectionRulesQueryData,
    RiskListCustomDetectionRulesQueryError
  >,
): UseQueryResult<
  RiskListCustomDetectionRulesQueryData,
  RiskListCustomDetectionRulesQueryError
>;
/**
 * listCustomDetectionRules risk
 *
 * @remarks
 * List custom detection rules for the current project.
 */
export declare function useRiskListCustomDetectionRulesSuspense(
  request?: ListCustomDetectionRulesRequest | undefined,
  security?: ListCustomDetectionRulesSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskListCustomDetectionRulesQueryData,
    RiskListCustomDetectionRulesQueryError
  >,
): UseSuspenseQueryResult<
  RiskListCustomDetectionRulesQueryData,
  RiskListCustomDetectionRulesQueryError
>;
export declare function setRiskListCustomDetectionRulesData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskListCustomDetectionRulesQueryData,
): RiskListCustomDetectionRulesQueryData | undefined;
export declare function invalidateRiskListCustomDetectionRules(
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
export declare function invalidateAllRiskListCustomDetectionRules(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskListCustomDetectionRules.d.ts.map
