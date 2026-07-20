import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CaptureEventResult } from "../models/components/captureeventresult.js";
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
  SubmitFeedbackRequest,
  SubmitFeedbackSecurity,
} from "../models/operations/submitfeedback.js";
import { MutationHookOptions } from "./_types.js";
export type ChatSubmitFeedbackMutationVariables = {
  request: SubmitFeedbackRequest;
  security?: SubmitFeedbackSecurity | undefined;
  options?: RequestOptions;
};
export type ChatSubmitFeedbackMutationData = CaptureEventResult;
export type ChatSubmitFeedbackMutationError =
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
 * submitFeedback chat
 *
 * @remarks
 * Submit user feedback for a chat (success/failure)
 */
export declare function useChatSubmitFeedbackMutation(
  options?: MutationHookOptions<
    ChatSubmitFeedbackMutationData,
    ChatSubmitFeedbackMutationError,
    ChatSubmitFeedbackMutationVariables
  >,
): UseMutationResult<
  ChatSubmitFeedbackMutationData,
  ChatSubmitFeedbackMutationError,
  ChatSubmitFeedbackMutationVariables
>;
export declare function mutationKeyChatSubmitFeedback(): MutationKey;
export declare function buildChatSubmitFeedbackMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ChatSubmitFeedbackMutationVariables,
  ) => Promise<ChatSubmitFeedbackMutationData>;
};
//# sourceMappingURL=chatSubmitFeedback.d.ts.map
