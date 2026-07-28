import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
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
  DeleteRiskEvalReviewRequest,
  DeleteRiskEvalReviewSecurity,
} from "../models/operations/deleteriskevalreview.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * deleteRiskEvalReview risk
 *
 * @remarks
 * Remove the current reviewer's verdict for one session (the toggle-off path). A reviewer can only clear their own verdict.
 */
export declare function riskEvalsDeleteReview(
  client: GramCore,
  request: DeleteRiskEvalReviewRequest,
  security?: DeleteRiskEvalReviewSecurity | undefined,
  options?: RequestOptions,
): APIPromise<
  Result<
    void,
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
//# sourceMappingURL=riskEvalsDeleteReview.d.ts.map
