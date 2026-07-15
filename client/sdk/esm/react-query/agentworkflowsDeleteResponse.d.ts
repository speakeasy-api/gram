import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
import { MutationHookOptions } from "./_types.js";
export type AgentworkflowsDeleteResponseMutationVariables = {
  request: operations.DeleteResponseRequest;
  security?: operations.DeleteResponseSecurity | undefined;
  options?: RequestOptions;
};
export type AgentworkflowsDeleteResponseMutationData = void;
export type AgentworkflowsDeleteResponseMutationError =
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
 * deleteResponse agentworkflows
 *
 * @remarks
 * Deletes any response associated with a given agent run.
 */
export declare function useAgentworkflowsDeleteResponseMutation(
  options?: MutationHookOptions<
    AgentworkflowsDeleteResponseMutationData,
    AgentworkflowsDeleteResponseMutationError,
    AgentworkflowsDeleteResponseMutationVariables
  >,
): UseMutationResult<
  AgentworkflowsDeleteResponseMutationData,
  AgentworkflowsDeleteResponseMutationError,
  AgentworkflowsDeleteResponseMutationVariables
>;
export declare function mutationKeyAgentworkflowsDeleteResponse(): MutationKey;
export declare function buildAgentworkflowsDeleteResponseMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: AgentworkflowsDeleteResponseMutationVariables,
  ) => Promise<AgentworkflowsDeleteResponseMutationData>;
};
//# sourceMappingURL=agentworkflowsDeleteResponse.d.ts.map
