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
  ListRiskResultsByChatRequest,
  ListRiskResultsByChatSecurity,
} from "../models/operations/listriskresultsbychat.js";
import {
  QueryHookOptions,
  SuspenseQueryHookOptions,
  TupleToPrefixes,
} from "./_types.js";
import {
  buildRiskListResultsByChatQuery,
  prefetchRiskListResultsByChat,
  queryKeyRiskListResultsByChat,
  RiskListResultsByChatQueryData,
} from "./riskListResultsByChat.core.js";
export {
  buildRiskListResultsByChatQuery,
  prefetchRiskListResultsByChat,
  queryKeyRiskListResultsByChat,
  type RiskListResultsByChatQueryData,
};
export type RiskListResultsByChatQueryError =
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
 * listRiskResultsByChat risk
 *
 * @remarks
 * List risk results grouped by chat session for the current project.
 */
export declare function useRiskListResultsByChat(
  request?: ListRiskResultsByChatRequest | undefined,
  security?: ListRiskResultsByChatSecurity | undefined,
  options?: QueryHookOptions<
    RiskListResultsByChatQueryData,
    RiskListResultsByChatQueryError
  >,
): UseQueryResult<
  RiskListResultsByChatQueryData,
  RiskListResultsByChatQueryError
>;
/**
 * listRiskResultsByChat risk
 *
 * @remarks
 * List risk results grouped by chat session for the current project.
 */
export declare function useRiskListResultsByChatSuspense(
  request?: ListRiskResultsByChatRequest | undefined,
  security?: ListRiskResultsByChatSecurity | undefined,
  options?: SuspenseQueryHookOptions<
    RiskListResultsByChatQueryData,
    RiskListResultsByChatQueryError
  >,
): UseSuspenseQueryResult<
  RiskListResultsByChatQueryData,
  RiskListResultsByChatQueryError
>;
export declare function setRiskListResultsByChatData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      cursor?: string | undefined;
      limit?: number | undefined;
      gramKey?: string | undefined;
      gramSession?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: RiskListResultsByChatQueryData,
): RiskListResultsByChatQueryData | undefined;
export declare function invalidateRiskListResultsByChat(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
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
export declare function invalidateAllRiskListResultsByChat(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=riskListResultsByChat.d.ts.map
