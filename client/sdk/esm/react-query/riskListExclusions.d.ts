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
  ListRiskExclusionsRequest,
  ListRiskExclusionsSecurity,
} from "../models/operations/listriskexclusions.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskListExclusionsQuery,
  prefetchRiskListExclusions,
  queryKeyRiskListExclusions,
  RiskListExclusionsQueryData,
} from "./riskListExclusions.core.js";
export {
  buildRiskListExclusionsQuery,
  prefetchRiskListExclusions,
  queryKeyRiskListExclusions,
  type RiskListExclusionsQueryData,
};
export type RiskListExclusionsQueryError =
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
 * listRiskExclusions risk
 *
 * @remarks
 * List risk exclusions for the current project. Optionally filter to a single policy.
 */
export declare function useRiskListExclusions(
  request?: ListRiskExclusionsRequest | undefined,
  security?: ListRiskExclusionsSecurity | undefined,
  options?: QueryHookOptions<
    RiskListExclusionsQueryData,
    RiskListExclusionsQueryError
  >,
): UseQueryResult<RiskListExclusionsQueryData, RiskListExclusionsQueryError>;
/**
 * listRiskExclusions risk
 *
 * @remarks
 * List risk exclusions for the current project. Optionally filter to a single policy.
 */
export declare function useRiskListExclusionsSuspense(
  request?: ListRiskExclusionsRequest | undefined,
  security?: ListRiskExclusionsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskListExclusionsQueryData,
    RiskListExclusionsQueryError
  >,
): UseSuspenseQueryResult<
  RiskListExclusionsQueryData,
  RiskListExclusionsQueryError
>;
export declare function setRiskListExclusionsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      riskPolicyId?: string | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskListExclusionsQueryData,
): RiskListExclusionsQueryData | undefined;
export declare function invalidateRiskListExclusions(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        riskPolicyId?: string | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskListExclusions(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskListExclusions.d.ts.map
