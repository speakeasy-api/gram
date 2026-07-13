import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { DeclineRiskPolicyChallengeResponseBody } from "../models/components/declineriskpolicychallengeresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeclineRiskPolicyChallengeRequest, DeclineRiskPolicyChallengeSecurity } from "../models/operations/declineriskpolicychallenge.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * declineRiskPolicyChallenge risk
 *
 * @remarks
 * Decline a risk policy warn/challenge from a warning-link token: invalidate the link and mark the challenge declined. The blocked action stays blocked.
 */
export declare function riskPolicyChallengesDecline(client: GramCore, request: DeclineRiskPolicyChallengeRequest, security?: DeclineRiskPolicyChallengeSecurity | undefined, options?: RequestOptions): APIPromise<Result<DeclineRiskPolicyChallengeResponseBody, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPolicyChallengesDecline.d.ts.map