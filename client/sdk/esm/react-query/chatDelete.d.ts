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
  DeleteChatRequest,
  DeleteChatSecurity,
} from "../models/operations/deletechat.js";
import { MutationHookOptions } from "./_types.js";
export type ChatDeleteMutationVariables = {
  request: DeleteChatRequest;
  security?: DeleteChatSecurity | undefined;
  options?: RequestOptions;
};
export type ChatDeleteMutationData = void;
export type ChatDeleteMutationError =
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
 * deleteChat chat
 *
 * @remarks
 * Soft-delete a chat by its ID
 */
export declare function useChatDeleteMutation(
  options?: MutationHookOptions<
    ChatDeleteMutationData,
    ChatDeleteMutationError,
    ChatDeleteMutationVariables
  >,
): UseMutationResult<
  ChatDeleteMutationData,
  ChatDeleteMutationError,
  ChatDeleteMutationVariables
>;
export declare function mutationKeyChatDelete(): MutationKey;
export declare function buildChatDeleteMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ChatDeleteMutationVariables,
  ) => Promise<ChatDeleteMutationData>;
};
//# sourceMappingURL=chatDelete.d.ts.map
