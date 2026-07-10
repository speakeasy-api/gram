import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { SendMessageResult } from "../models/components/sendmessageresult.js";
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
  SendAssistantMessageRequest,
  SendAssistantMessageSecurity,
} from "../models/operations/sendassistantmessage.js";
import { MutationHookOptions } from "./_types.js";
export type SendAssistantMessageMutationVariables = {
  request: SendAssistantMessageRequest;
  security?: SendAssistantMessageSecurity | undefined;
  options?: RequestOptions;
};
export type SendAssistantMessageMutationData = SendMessageResult;
export type SendAssistantMessageMutationError =
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
 * sendMessage assistants
 *
 * @remarks
 * Send a message from the dashboard to an assistant as the calling user. Continue an existing conversation by passing its chat_id (from listChats), or omit chat_id to start a new conversation — the server mints and returns a fresh chat id. The reply is delivered asynchronously; poll the chat service (loadChat) to read it.
 */
export declare function useSendAssistantMessageMutation(
  options?: MutationHookOptions<
    SendAssistantMessageMutationData,
    SendAssistantMessageMutationError,
    SendAssistantMessageMutationVariables
  >,
): UseMutationResult<
  SendAssistantMessageMutationData,
  SendAssistantMessageMutationError,
  SendAssistantMessageMutationVariables
>;
export declare function mutationKeySendAssistantMessage(): MutationKey;
export declare function buildSendAssistantMessageMutation(
  client$: GramCore,
  hookOptions?: RequestOptions,
): {
  mutationKey: MutationKey;
  mutationFn: (
    variables: SendAssistantMessageMutationVariables,
  ) => Promise<SendAssistantMessageMutationData>;
};
//# sourceMappingURL=sendAssistantMessage.d.ts.map
