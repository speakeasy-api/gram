import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
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
  EnsureManagedAssistantRequest,
  EnsureManagedAssistantSecurity,
} from "../models/operations/ensuremanagedassistant.js";
import { MutationHookOptions } from "./_types.js";
export type EnsureManagedAssistantMutationVariables = {
  request?: EnsureManagedAssistantRequest | undefined;
  security?: EnsureManagedAssistantSecurity | undefined;
  options?: RequestOptions;
};
export type EnsureManagedAssistantMutationData = Assistant;
export type EnsureManagedAssistantMutationError =
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
 * ensureManagedAssistant assistants
 *
 * @remarks
 * Get the project's built-in Project Assistant, provisioning it on first access. Idempotent — safe to call on every sidebar open.
 */
export declare function useEnsureManagedAssistantMutation(
  options?: MutationHookOptions<
    EnsureManagedAssistantMutationData,
    EnsureManagedAssistantMutationError,
    EnsureManagedAssistantMutationVariables
  >,
): UseMutationResult<
  EnsureManagedAssistantMutationData,
  EnsureManagedAssistantMutationError,
  EnsureManagedAssistantMutationVariables
>;
export declare function mutationKeyEnsureManagedAssistant(): MutationKey;
export declare function buildEnsureManagedAssistantMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: EnsureManagedAssistantMutationVariables,
  ) => Promise<EnsureManagedAssistantMutationData>;
};
//# sourceMappingURL=ensureManagedAssistant.d.ts.map
