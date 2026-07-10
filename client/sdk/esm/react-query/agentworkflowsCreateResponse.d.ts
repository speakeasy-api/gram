import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import * as errors from "../models/errors/index.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import * as operations from "../models/operations/index.js";
import { MutationHookOptions } from "./_types.js";
export type AgentworkflowsCreateResponseMutationVariables = {
    request: operations.CreateResponseRequest;
    security?: operations.CreateResponseSecurity | undefined;
    options?: RequestOptions;
};
export type AgentworkflowsCreateResponseMutationData = components.WorkflowAgentResponseOutput;
export type AgentworkflowsCreateResponseMutationError = errors.ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * createResponse agentworkflows
 *
 * @remarks
 * Create a new agent response. Executes an agent workflow with the provided input and tools.
 */
export declare function useAgentworkflowsCreateResponseMutation(options?: MutationHookOptions<AgentworkflowsCreateResponseMutationData, AgentworkflowsCreateResponseMutationError, AgentworkflowsCreateResponseMutationVariables>): UseMutationResult<AgentworkflowsCreateResponseMutationData, AgentworkflowsCreateResponseMutationError, AgentworkflowsCreateResponseMutationVariables>;
export declare function mutationKeyAgentworkflowsCreateResponse(): MutationKey;
export declare function buildAgentworkflowsCreateResponseMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: AgentworkflowsCreateResponseMutationVariables) => Promise<AgentworkflowsCreateResponseMutationData>;
};
//# sourceMappingURL=agentworkflowsCreateResponse.d.ts.map