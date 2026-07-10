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
  GetRiskPolicyStatusRequest,
  GetRiskPolicyStatusSecurity,
} from "../models/operations/getriskpolicystatus.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskPoliciesStatusQuery,
  prefetchRiskPoliciesStatus,
  queryKeyRiskPoliciesStatus,
  RiskPoliciesStatusQueryData,
} from "./riskPoliciesStatus.core.js";
export {
  buildRiskPoliciesStatusQuery,
  prefetchRiskPoliciesStatus,
  queryKeyRiskPoliciesStatus,
  type RiskPoliciesStatusQueryData,
};
export type RiskPoliciesStatusQueryError =
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
 * getRiskPolicyStatus risk
 *
 * @remarks
 * Get the analysis status of a risk policy including progress and workflow state.
 */
export declare function useRiskPoliciesStatus(
  request: GetRiskPolicyStatusRequest,
  security?: GetRiskPolicyStatusSecurity | undefined,
  options?: QueryHookOptions<
    RiskPoliciesStatusQueryData,
    RiskPoliciesStatusQueryError
  >,
): UseQueryResult<RiskPoliciesStatusQueryData, RiskPoliciesStatusQueryError>;
/**
 * getRiskPolicyStatus risk
 *
 * @remarks
 * Get the analysis status of a risk policy including progress and workflow state.
 */
export declare function useRiskPoliciesStatusSuspense(
  request: GetRiskPolicyStatusRequest,
  security?: GetRiskPolicyStatusSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskPoliciesStatusQueryData,
    RiskPoliciesStatusQueryError
  >,
): UseSuspenseQueryResult<
  RiskPoliciesStatusQueryData,
  RiskPoliciesStatusQueryError
>;
export declare function setRiskPoliciesStatusData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskPoliciesStatusQueryData,
): RiskPoliciesStatusQueryData | undefined;
export declare function invalidateRiskPoliciesStatus(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskPoliciesStatus(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskPoliciesStatus.d.ts.map
