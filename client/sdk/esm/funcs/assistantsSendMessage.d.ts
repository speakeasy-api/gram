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
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * sendMessage assistants
 *
 * @remarks
 * Send a message from the dashboard to an assistant as the calling user. Continue an existing conversation by passing its chat_id (from listChats), or omit chat_id to start a new conversation — the server mints and returns a fresh chat id. The reply is delivered asynchronously; poll the chat service (loadChat) to read it.
 */
export declare function assistantsSendMessage(
  client: GramCore,
  request: SendAssistantMessageRequest,
  security?: SendAssistantMessageSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    SendMessageResult,
    | ServiceError
    | GramError
    | ResponseValidationError
    | ConnectionError
    | RequestAbortedError
    | RequestTimeoutError
    | InvalidRequestError
    | UnexpectedClientError
    | SDKValidationError
  >
>;
//# sourceMappingURL=assistantsSendMessage.d.ts.map
