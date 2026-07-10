import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CreateResponseBody } from "../models/components/createresponsebody.js";
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
  CreateChatSessionRequest,
  CreateChatSessionSecurity,
} from "../models/operations/createchatsession.js";
import { MutationHookOptions } from "./_types.js";
export type ChatSessionsCreateMutationVariables = {
  request: CreateChatSessionRequest;
  security?: CreateChatSessionSecurity | undefined;
  options?: RequestOptions;
};
export type ChatSessionsCreateMutationData = CreateResponseBody;
export type ChatSessionsCreateMutationError =
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
 * create chatSessions
 *
 * @remarks
 * Creates a new chat session token
 */
export declare function useChatSessionsCreateMutation(
  options?: MutationHookOptions<
    ChatSessionsCreateMutationData,
    ChatSessionsCreateMutationError,
    ChatSessionsCreateMutationVariables
  >,
): UseMutationResult<
  ChatSessionsCreateMutationData,
  ChatSessionsCreateMutationError,
  ChatSessionsCreateMutationVariables
>;
export declare function mutationKeyChatSessionsCreate(): MutationKey;
export declare function buildChatSessionsCreateMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: ChatSessionsCreateMutationVariables,
  ) => Promise<ChatSessionsCreateMutationData>;
};
//# sourceMappingURL=chatSessionsCreate.d.ts.map
