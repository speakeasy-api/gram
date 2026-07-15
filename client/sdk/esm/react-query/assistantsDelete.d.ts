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
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import {
  DeleteAssistantRequest,
  DeleteAssistantSecurity,
} from "../models/operations/deleteassistant.js";
import { MutationHookOptions } from "./_types.js";
export type AssistantsDeleteMutationVariables = {
  request: DeleteAssistantRequest;
  security?: DeleteAssistantSecurity | undefined;
  options?: RequestOptions;
};
export type AssistantsDeleteMutationData = void;
export type AssistantsDeleteMutationError =
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
 * deleteAssistant assistants
 *
 * @remarks
 * Delete an assistant.
 */
export declare function useAssistantsDeleteMutation(
  options?: MutationHookOptions<
    AssistantsDeleteMutationData,
    AssistantsDeleteMutationError,
    AssistantsDeleteMutationVariables
  >,
): UseMutationResult<
  AssistantsDeleteMutationData,
  AssistantsDeleteMutationError,
  AssistantsDeleteMutationVariables
>;
export declare function mutationKeyAssistantsDelete(): MutationKey;
export declare function buildAssistantsDeleteMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: AssistantsDeleteMutationVariables,
  ) => Promise<AssistantsDeleteMutationData>;
};
//# sourceMappingURL=assistantsDelete.d.ts.map
