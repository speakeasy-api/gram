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
  ListRiskResultsRequest,
  ListRiskResultsSecurity,
} from "../models/operations/listriskresults.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskListResultsQuery,
  prefetchRiskListResults,
  queryKeyRiskListResults,
  RiskListResultsQueryData,
} from "./riskListResults.core.js";
export {
  buildRiskListResultsQuery,
  prefetchRiskListResults,
  queryKeyRiskListResults,
  type RiskListResultsQueryData,
};
export type RiskListResultsQueryError =
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
 * listRiskResults risk
 *
 * @remarks
 * List risk analysis results for the current project.
 */
export declare function useRiskListResults(
  request?: ListRiskResultsRequest | undefined,
  security?: ListRiskResultsSecurity | undefined,
  options?: QueryHookOptions<
    RiskListResultsQueryData,
    RiskListResultsQueryError
  >,
): UseQueryResult<RiskListResultsQueryData, RiskListResultsQueryError>;
/**
 * listRiskResults risk
 *
 * @remarks
 * List risk analysis results for the current project.
 */
export declare function useRiskListResultsSuspense(
  request?: ListRiskResultsRequest | undefined,
  security?: ListRiskResultsSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskListResultsQueryData,
    RiskListResultsQueryError
  >,
): UseSuspenseQueryResult<RiskListResultsQueryData, RiskListResultsQueryError>;
export declare function setRiskListResultsData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      policyId?: string | undefined;
      chatId?: string | undefined;
      category?: string | undefined;
      ruleId?: string | undefined;
      userId?: string | undefined;
      uniqueMatch?: boolean | undefined;
      from?: Date | undefined;
      to?: Date | undefined;
      cursor?: string | undefined;
      limit?: number | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskListResultsQueryData,
): RiskListResultsQueryData | undefined;
export declare function invalidateRiskListResults(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        policyId?: string | undefined;
        chatId?: string | undefined;
        category?: string | undefined;
        ruleId?: string | undefined;
        userId?: string | undefined;
        uniqueMatch?: boolean | undefined;
        from?: Date | undefined;
        to?: Date | undefined;
        cursor?: string | undefined;
        limit?: number | undefined;
        gramKey?: string | undefined;
        gramSession?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskListResults(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskListResults.d.ts.map
