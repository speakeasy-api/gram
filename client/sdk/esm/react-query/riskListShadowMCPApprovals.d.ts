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
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskListShadowMCPApprovalsQuery,
  prefetchRiskListShadowMCPApprovals,
  queryKeyRiskListShadowMCPApprovals,
  RiskListShadowMCPApprovalsQueryData,
} from "./riskListShadowMCPApprovals.core.js";
export {
  buildRiskListShadowMCPApprovalsQuery,
  prefetchRiskListShadowMCPApprovals,
  queryKeyRiskListShadowMCPApprovals,
  type RiskListShadowMCPApprovalsQueryData,
};
export type RiskListShadowMCPApprovalsQueryError =
  | errors.ServiceError
  | GramError
  | ResponseValidationError
  | ConnectionError
  | RequestAbortedError
  | RequestTimeoutError
  | InvalidRequestError
  | UnexpectedClientError
  | SDKValidationError;
/**
 * listShadowMCPApprovals risk
 *
 * @remarks
 * List shadow-MCP approvals (URL- or command-keyed) for a policy. Temporary Redis-backed storage; will move to a dedicated table once the feature graduates.
 */
export declare function useRiskListShadowMCPApprovals(
  request: operations.ListShadowMCPApprovalsRequest,
  security?: operations.ListShadowMCPApprovalsSecurity | undefined,
  options?: QueryHookOptions<
    RiskListShadowMCPApprovalsQueryData,
    RiskListShadowMCPApprovalsQueryError
  >,
): UseQueryResult<
  RiskListShadowMCPApprovalsQueryData,
  RiskListShadowMCPApprovalsQueryError
>;
/**
 * listShadowMCPApprovals risk
 *
 * @remarks
 * List shadow-MCP approvals (URL- or command-keyed) for a policy. Temporary Redis-backed storage; will move to a dedicated table once the feature graduates.
 */
export declare function useRiskListShadowMCPApprovalsSuspense(
  request: operations.ListShadowMCPApprovalsRequest,
  security?: operations.ListShadowMCPApprovalsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskListShadowMCPApprovalsQueryData,
    RiskListShadowMCPApprovalsQueryError
  >,
): UseSuspenseQueryResult<
  RiskListShadowMCPApprovalsQueryData,
  RiskListShadowMCPApprovalsQueryError
>;
export declare function setRiskListShadowMCPApprovalsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      policyId: string;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskListShadowMCPApprovalsQueryData,
): RiskListShadowMCPApprovalsQueryData | undefined;
export declare function invalidateRiskListShadowMCPApprovals(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        policyId: string;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskListShadowMCPApprovals(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskListShadowMCPApprovals.d.ts.map
