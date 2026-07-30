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
export type KickoffAssistantMessageMutationVariables = {
  request: operations.KickoffAssistantMessageRequest;
  security?: operations.KickoffAssistantMessageSecurity | undefined;
  options?: RequestOptions;
};
export type KickoffAssistantMessageMutationData = components.SendMessageResult;
export type KickoffAssistantMessageMutationError =
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
 * kickoffMessage assistants
 *
 * @remarks
 * Nudge the assistant to proactively greet a returning user. Enqueues a hidden turn (the prompt is server-owned and never shown in the conversation log) so the assistant emits a short welcome-back recap as the next reply. Poll the returned chat to read it.
 */
export declare function useKickoffAssistantMessageMutation(
  options?: MutationHookOptions<
    KickoffAssistantMessageMutationData,
    KickoffAssistantMessageMutationError,
    KickoffAssistantMessageMutationVariables
  >,
): UseMutationResult<
  KickoffAssistantMessageMutationData,
  KickoffAssistantMessageMutationError,
  KickoffAssistantMessageMutationVariables
>;
export declare function mutationKeyKickoffAssistantMessage(): MutationKey;
export declare function buildKickoffAssistantMessageMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: KickoffAssistantMessageMutationVariables,
  ) => Promise<KickoffAssistantMessageMutationData>;
};
//# sourceMappingURL=kickoffAssistantMessage.d.ts.map
