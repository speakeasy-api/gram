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
export type AgentsDeleteMutationVariables = {
  request: operations.DeleteAgentResponseRequest;
  security?: operations.DeleteAgentResponseSecurity | undefined;
  options?: RequestOptions;
};
export type AgentsDeleteMutationData = void;
export type AgentsDeleteMutationError =
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
 * deleteResponse agents
 *
 * @remarks
 * Deletes any response associated with a given agent run.
 */
export declare function useAgentsDeleteMutation(
  options?: MutationHookOptions<
    AgentsDeleteMutationData,
    AgentsDeleteMutationError,
    AgentsDeleteMutationVariables
  >,
): UseMutationResult<
  AgentsDeleteMutationData,
  AgentsDeleteMutationError,
  AgentsDeleteMutationVariables
>;
export declare function mutationKeyAgentsDelete(): MutationKey;
export declare function buildAgentsDeleteMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: AgentsDeleteMutationVariables,
  ) => Promise<AgentsDeleteMutationData>;
};
//# sourceMappingURL=agentsDelete.d.ts.map
