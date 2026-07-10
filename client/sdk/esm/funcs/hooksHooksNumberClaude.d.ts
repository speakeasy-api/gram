import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ClaudeHookResult } from "../models/components/claudehookresult.js";
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
import { HooksNumberClaudeRequest } from "../models/operations/hooksnumberclaude.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * claude hooks
 *
 * @remarks
 * Unified endpoint for all Claude Code hook events. Handles SessionStart, PreToolUse, PostToolUse, and PostToolUseFailure.
 */
export declare function hooksHooksNumberClaude(
  client: GramCore,
  request: HooksNumberClaudeRequest,
  options?: RequestOptions,
): APIPromise<
  Result<
    ClaudeHookResult,
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
//# sourceMappingURL=hooksHooksNumberClaude.d.ts.map
