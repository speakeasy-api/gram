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
  AgentsGetQueryData,
  buildAgentsGetQuery,
  prefetchAgentsGet,
  queryKeyAgentsGet,
} from "./agentsGet.core.js";
export {
  type AgentsGetQueryData,
  buildAgentsGetQuery,
  prefetchAgentsGet,
  queryKeyAgentsGet,
};
export type AgentsGetQueryError =
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
 * getResponse agents
 *
 * @remarks
 * Get the status of an async agent response by its ID.
 */
export declare function useAgentsGet(
  request: operations.GetAgentResponseRequest,
  security?: operations.GetAgentResponseSecurity | undefined,
  options?: QueryHookOptions<AgentsGetQueryData, AgentsGetQueryError>,
): UseQueryResult<AgentsGetQueryData, AgentsGetQueryError>;
/**
 * getResponse agents
 *
 * @remarks
 * Get the status of an async agent response by its ID.
 */
export declare function useAgentsGetSuspense(
  request: operations.GetAgentResponseRequest,
  security?: operations.GetAgentResponseSecurity | undefined,
  options?: SuspenseQueryHookOptions<AgentsGetQueryData, AgentsGetQueryError>,
): UseSuspenseQueryResult<AgentsGetQueryData, AgentsGetQueryError>;
export declare function setAgentsGetData(
  client: QueryClient,
  queryKeyBase: [
    parameters: {
      responseId: string;
      gramKey?: string | undefined;
      gramProject?: string | undefined;
    },
  ],
  data: AgentsGetQueryData,
): AgentsGetQueryData | undefined;
export declare function invalidateAgentsGet(
  client: QueryClient,
  queryKeyBase: TupleToPrefixes<
    [
      parameters: {
        responseId: string;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
      },
    ]
  >,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
export declare function invalidateAllAgentsGet(
  client: QueryClient,
  filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">,
): Promise<void>;
//# sourceMappingURL=agentsGet.d.ts.map
