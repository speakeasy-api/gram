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
  GetRiskBlockRequest,
  GetRiskBlockSecurity,
} from "../models/operations/getriskblock.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskGetBlockQuery,
  prefetchRiskGetBlock,
  queryKeyRiskGetBlock,
  RiskGetBlockQueryData,
} from "./riskGetBlock.core.js";
export {
  buildRiskGetBlockQuery,
  prefetchRiskGetBlock,
  queryKeyRiskGetBlock,
  type RiskGetBlockQueryData,
};
export type RiskGetBlockQueryError =
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
 * getRiskBlock risk
 *
 * @remarks
 * Get a tool call block by its risk result ID for the durable block page.
 */
export declare function useRiskGetBlock(
  request: GetRiskBlockRequest,
  security?: GetRiskBlockSecurity | undefined,
  options?: QueryHookOptions<RiskGetBlockQueryData, RiskGetBlockQueryError>,
): UseQueryResult<RiskGetBlockQueryData, RiskGetBlockQueryError>;
/**
 * getRiskBlock risk
 *
 * @remarks
 * Get a tool call block by its risk result ID for the durable block page.
 */
export declare function useRiskGetBlockSuspense(
  request: GetRiskBlockRequest,
  security?: GetRiskBlockSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskGetBlockQueryData,
    RiskGetBlockQueryError
  >,
): UseSuspenseQueryResult<RiskGetBlockQueryData, RiskGetBlockQueryError>;
export declare function setRiskGetBlockData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      id: string;
      gramSession?: string | undefined;
    },
  ],
  data: RiskGetBlockQueryData,
): RiskGetBlockQueryData | undefined;
export declare function invalidateRiskGetBlock(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        id: string;
        gramSession?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllRiskGetBlock(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskGetBlock.d.ts.map
