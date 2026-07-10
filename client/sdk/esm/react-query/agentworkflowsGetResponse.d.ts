import { InvalidateQueryFilters, QueryClient, UseQueryResult, UseSuspenseQueryResult } from "@tanstack/react-query";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { QueryHookOptions, SuspenseQueryHookOptions, TupleToPrefixes } from "./_types.js";
import { AgentworkflowsGetResponseQueryData, buildAgentworkflowsGetResponseQuery, prefetchAgentworkflowsGetResponse, queryKeyAgentworkflowsGetResponse } from "./agentworkflowsGetResponse.core.js";
export { type AgentworkflowsGetResponseQueryData, buildAgentworkflowsGetResponseQuery, prefetchAgentworkflowsGetResponse, queryKeyAgentworkflowsGetResponse, };
export type AgentworkflowsGetResponseQueryError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getResponse agentworkflows
 *
 * @remarks
 * Get the status of an async agent response by its ID.
 */
export declare function useAgentworkflowsGetResponse(request: operations.GetResponseRequest, security?: operations.GetResponseSecurity | undefined, options?: QueryHookOptions<AgentworkflowsGetResponseQueryData, AgentworkflowsGetResponseQueryError>): UseQueryResult<AgentworkflowsGetResponseQueryData, AgentworkflowsGetResponseQueryError>;
/**
 * getResponse agentworkflows
 *
 * @remarks
 * Get the status of an async agent response by its ID.
 */
export declare function useAgentworkflowsGetResponseSuspense(request: operations.GetResponseRequest, security?: operations.GetResponseSecurity | undefined, options?: SuspenseQueryHookOptions<AgentworkflowsGetResponseQueryData, AgentworkflowsGetResponseQueryError>): UseSuspenseQueryResult<AgentworkflowsGetResponseQueryData, AgentworkflowsGetResponseQueryError>;
export declare function setAgentworkflowsGetResponseData(client: QueryClient, queryKeyBase: [
    parameters: {
        responseId: string;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
], data: AgentworkflowsGetResponseQueryData): AgentworkflowsGetResponseQueryData | undefined;
export declare function invalidateAgentworkflowsGetResponse(client: QueryClient, queryKeyBase: TupleToPrefixes<[
    parameters: {
        responseId: string;
        gramKey?: string | undefined;
        gramProject?: string | undefined;
    }
]>, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
export declare function invalidateAllAgentworkflowsGetResponse(client: QueryClient, filters?: Omit<InvalidateQueryFilters, "queryKey" | "predicate" | "exact">): Promise<void>;
//# sourceMappingURL=agentworkflowsGetResponse.d.ts.map