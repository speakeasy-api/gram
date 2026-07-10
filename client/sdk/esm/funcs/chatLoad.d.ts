import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Chat } from "../models/components/chat.js";
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
  LoadChatRequest,
  LoadChatSecurity,
} from "../models/operations/loadchat.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * loadChat chat
 *
 * @remarks
 * Load a chat by its ID. Messages within a generation are paginated by `seq` keyset: omit cursors to receive the newest page, pass `before_seq` to load older messages (scroll up) or `after_seq` to load newer ones (scroll down). Set `from_start` to receive the oldest page (the start of the thread) instead of the newest. Omit `generation` to receive the latest generation. Set `risk_only` to return only messages with risk findings plus a few messages of surrounding context per finding. Set `query` to instead return only messages whose text matches a search query plus surrounding context (mutually exclusive with `risk_only`).
 */
export declare function chatLoad(
  client: GramCore,
  request: LoadChatRequest,
  security?: LoadChatSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    Chat,
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
//# sourceMappingURL=chatLoad.d.ts.map
