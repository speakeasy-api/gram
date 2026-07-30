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
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * kickoffMessage assistants
 *
 * @remarks
 * Nudge the assistant to proactively greet a returning user. Enqueues a hidden turn (the prompt is server-owned and never shown in the conversation log) so the assistant emits a short welcome-back recap as the next reply. Poll the returned chat to read it.
 */
export declare function assistantsKickoffMessage(
  client: GramCore,
  request: operations.KickoffAssistantMessageRequest,
  security?: operations.KickoffAssistantMessageSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    components.SendMessageResult,
    | errors.ServiceError
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
//# sourceMappingURL=assistantsKickoffMessage.d.ts.map
