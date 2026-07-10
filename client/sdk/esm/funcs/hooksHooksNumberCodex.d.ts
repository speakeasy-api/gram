import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CodexHookResult } from "../models/components/codexhookresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { HooksNumberCodexRequest, HooksNumberCodexSecurity } from "../models/operations/hooksnumbercodex.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * codex hooks
 *
 * @remarks
 * Endpoint for Codex hook events. Handles SessionStart, PreToolUse, PermissionRequest, PostToolUse, UserPromptSubmit, and Stop.
 */
export declare function hooksHooksNumberCodex(client: GramCore, request: HooksNumberCodexRequest, security?: HooksNumberCodexSecurity | undefined, options?: RequestOptions): APIPromise<Result<CodexHookResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=hooksHooksNumberCodex.d.ts.map