import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetRiskPolicyChallengeResponseBody } from "../models/components/getriskpolicychallengeresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRiskPolicyChallengeRequest, GetRiskPolicyChallengeSecurity } from "../models/operations/getriskpolicychallenge.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * getRiskPolicyChallenge risk
 *
 * @remarks
 * Fetch the details of a risk policy warn/challenge from a warning-link token, WITHOUT acknowledging it. Powers the approval page (shows what was flagged and Approve/Deny actions).
 */
export declare function riskPolicyChallengesGet(client: GramCore, request: GetRiskPolicyChallengeRequest, security?: GetRiskPolicyChallengeSecurity | undefined, options?: RequestOptions): APIPromise<Result<GetRiskPolicyChallengeResponseBody, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPolicyChallengesGet.d.ts.map