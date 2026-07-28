import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { CaptureEventResult } from "../models/components/captureeventresult.js";
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
  SubmitFeedbackRequest,
  SubmitFeedbackSecurity,
} from "../models/operations/submitfeedback.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * submitFeedback chat
 *
 * @remarks
 * Submit user feedback for a chat (success/failure)
 */
export declare function chatSubmitFeedback(
  client: GramCore,
  request: SubmitFeedbackRequest,
  security?: SubmitFeedbackSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    CaptureEventResult,
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
//# sourceMappingURL=chatSubmitFeedback.d.ts.map
