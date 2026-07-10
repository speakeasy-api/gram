import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { ListRiskEvalReviewsResult } from "../models/components/listriskevalreviewsresult.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { ListRiskEvalReviewsRequest, ListRiskEvalReviewsSecurity } from "../models/operations/listriskevalreviews.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * listRiskEvalReviews risk
 *
 * @remarks
 * List the active regression set for a prompt-based policy: every reviewer's current ground-truth verdicts.
 */
export declare function riskEvalsListReviews(client: GramCore, request: ListRiskEvalReviewsRequest, security?: ListRiskEvalReviewsSecurity | undefined, options?: RequestOptions): APIPromise<Result<ListRiskEvalReviewsResult, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskEvalsListReviews.d.ts.map