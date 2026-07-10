import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GenerateTitleResponseBody } from "../models/components/generatetitleresponsebody.js";
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
  GenerateTitleRequest,
  GenerateTitleSecurity,
} from "../models/operations/generatetitle.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * generateTitle chat
 *
 * @remarks
 * Read or set a chat's title. Omit `title` to return the current/auto-generated title (titles are generated asynchronously after a completion). Provide `title` to set a manual title that auto-generation will never overwrite; provide an empty `title` to clear the manual title and re-enable auto-generation.
 */
export declare function chatGenerateTitle(
  client: GramCore,
  request: GenerateTitleRequest,
  security?: GenerateTitleSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    GenerateTitleResponseBody,
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
//# sourceMappingURL=chatGenerateTitle.d.ts.map
