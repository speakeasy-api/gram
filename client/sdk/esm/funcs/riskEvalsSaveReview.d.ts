import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RiskPolicyEvalReview } from "../models/components/riskpolicyevalreview.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { SaveRiskEvalReviewRequest, SaveRiskEvalReviewSecurity } from "../models/operations/saveriskevalreview.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * saveRiskEvalReview risk
 *
 * @remarks
 * Record (or replace) the current reviewer's ground-truth verdict for one chat session under a prompt-based policy. This is the durable regression set the eval workbench scores the live guardrail against. Upserts: a reviewer has at most one verdict per session per policy.
 */
export declare function riskEvalsSaveReview(client: GramCore, request: SaveRiskEvalReviewRequest, security?: SaveRiskEvalReviewSecurity | undefined, options?: RequestOptions): APIPromise<Result<RiskPolicyEvalReview, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskEvalsSaveReview.d.ts.map