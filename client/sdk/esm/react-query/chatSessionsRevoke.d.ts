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
  RevokeChatSessionRequest,
  RevokeChatSessionSecurity,
} from "../models/operations/revokechatsession.js";
import { MutationHookOptions } from "./_types.js";
export type ChatSessionsRevokeMutationVariables = {
  request: RevokeChatSessionRequest;
  security?: RevokeChatSessionSecurity | undefined;
  options?: RequestOptions;
};
export type ChatSessionsRevokeMutationData = void;
export type ChatSessionsRevokeMutationError =
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
 * revoke chatSessions
 *
 * @remarks
 * Revokes an existing chat session token
 */
export declare function useChatSessionsRevokeMutation(
  options?: MutationHookOptions<
    ChatSessionsRevokeMutationData,
    ChatSessionsRevokeMutationError,
    ChatSessionsRevokeMutationVariables
  >,
): UseMutationResult<
  ChatSessionsRevokeMutationData,
  ChatSessionsRevokeMutationError,
  ChatSessionsRevokeMutationVariables
>;
export declare function mutationKeyChatSessionsRevoke(): MutationKey;
export declare function buildChatSessionsRevokeMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ChatSessionsRevokeMutationVariables,
  ) => Promise<ChatSessionsRevokeMutationData>;
};
//# sourceMappingURL=chatSessionsRevoke.d.ts.map
