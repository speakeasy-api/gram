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
  ListRiskPolicyBypassRequestsRequest,
  ListRiskPolicyBypassRequestsSecurity,
  QueryParamStatus,
} from "../models/operations/listriskpolicybypassrequests.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskListPolicyBypassRequestsQuery,
  prefetchRiskListPolicyBypassRequests,
  queryKeyRiskListPolicyBypassRequests,
  RiskListPolicyBypassRequestsQueryData,
} from "./riskListPolicyBypassRequests.core.js";
export {
  buildRiskListPolicyBypassRequestsQuery,
  prefetchRiskListPolicyBypassRequests,
  queryKeyRiskListPolicyBypassRequests,
  type RiskListPolicyBypassRequestsQueryData,
};
export type RiskListPolicyBypassRequestsQueryError =
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
 * listRiskPolicyBypassRequests risk
 *
 * @remarks
 * List current risk policy bypass request workflow records.
 */
export declare function useRiskListPolicyBypassRequests(
  request?: ListRiskPolicyBypassRequestsRequest | undefined,
  security?: ListRiskPolicyBypassRequestsSecurity | undefined,
  options?: QueryHookOptions<
    RiskListPolicyBypassRequestsQueryData,
    RiskListPolicyBypassRequestsQueryError
  >,
): UseQueryResult<
  RiskListPolicyBypassRequestsQueryData,
  RiskListPolicyBypassRequestsQueryError
>;
/**
 * listRiskPolicyBypassRequests risk
 *
 * @remarks
 * List current risk policy bypass request workflow records.
 */
export declare function useRiskListPolicyBypassRequestsSuspense(
  request?: ListRiskPolicyBypassRequestsRequest | undefined,
  security?: ListRiskPolicyBypassRequestsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskListPolicyBypassRequestsQueryData,
    RiskListPolicyBypassRequestsQueryError
  >,
): UseSuspenseQueryResult<
  RiskListPolicyBypassRequestsQueryData,
  RiskListPolicyBypassRequestsQueryError
>;
export declare function setRiskListPolicyBypassRequestsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      policyId?: string | undefined;
      status?: QueryParamStatus | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskListPolicyBypassRequestsQueryData,
): RiskListPolicyBypassRequestsQueryData | undefined;
export declare function invalidateRiskListPolicyBypassRequests(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        policyId?: string | undefined;
        status?: QueryParamStatus | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskListPolicyBypassRequests(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskListPolicyBypassRequests.d.ts.map
