import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
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
export type AgentsCreateMutationVariables = {
  request: operations.CreateAgentResponseRequest;
  security?: operations.CreateAgentResponseSecurity | undefined;
  options?: RequestOptions;
};
export type AgentsCreateMutationData = components.AgentResponseOutput;
export type AgentsCreateMutationError =
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
 * createResponse agents
 *
 * @remarks
 * Create a new agent response. Executes an agent workflow with the provided input and tools.
 */
export declare function useAgentsCreateMutation(
  options?: MutationHookOptions<
    AgentsCreateMutationData,
    AgentsCreateMutationError,
    AgentsCreateMutationVariables
  >,
): UseMutationResult<
  AgentsCreateMutationData,
  AgentsCreateMutationError,
  AgentsCreateMutationVariables
>;
export declare function mutationKeyAgentsCreate(): MutationKey;
export declare function buildAgentsCreateMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: AgentsCreateMutationVariables,
  ) => Promise<AgentsCreateMutationData>;
};
//# sourceMappingURL=agentsCreate.d.ts.map
