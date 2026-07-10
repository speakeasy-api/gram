import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SetChatPinnedRequest, SetChatPinnedSecurity } from "../models/operations/setchatpinned.js";
import { MutationHookOptions } from "./_types.js";
export type ChatSetPinnedMutationVariables = {
    request: SetChatPinnedRequest;
    security?: SetChatPinnedSecurity | undefined;
    options?: RequestOptions;
};
export type ChatSetPinnedMutationData = void;
export type ChatSetPinnedMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * setPinned chat
 *
 * @remarks
 * Pin or unpin a chat. Pinned chats surface in a dedicated section above recents on the chat page.
 */
export declare function useChatSetPinnedMutation(options?: MutationHookOptions<ChatSetPinnedMutationData, ChatSetPinnedMutationError, ChatSetPinnedMutationVariables>): UseMutationResult<ChatSetPinnedMutationData, ChatSetPinnedMutationError, ChatSetPinnedMutationVariables>;
export declare function mutationKeyChatSetPinned(): MutationKey;
export declare function buildChatSetPinnedMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: ChatSetPinnedMutationVariables) => Promise<ChatSetPinnedMutationData>;
};
//# sourceMappingURL=chatSetPinned.d.ts.map