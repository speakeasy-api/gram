import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CursorHookResult } from "../models/components/cursorhookresult.js";
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
  HooksNumberCursorRequest,
  HooksNumberCursorSecurity,
} from "../models/operations/hooksnumbercursor.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * cursor hooks
 *
 * @remarks
 * Endpoint for Cursor hook events. Handles beforeSubmitPrompt, stop, afterAgentResponse, afterAgentThought, preToolUse, postToolUse, postToolUseFailure, beforeMCPExecution, and afterMCPExecution.
 */
export declare function hooksHooksNumberCursor(
  client: GramCore,
  request: HooksNumberCursorRequest,
  security?: HooksNumberCursorSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    CursorHookResult,
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
//# sourceMappingURL=hooksHooksNumberCursor.d.ts.map
