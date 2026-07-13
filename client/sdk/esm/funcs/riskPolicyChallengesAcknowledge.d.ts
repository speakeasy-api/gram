import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AcknowledgeRiskPolicyChallengeResponseBody } from "../models/components/acknowledgeriskpolicychallengeresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AcknowledgeRiskPolicyChallengeRequest, AcknowledgeRiskPolicyChallengeSecurity } from "../models/operations/acknowledgeriskpolicychallenge.js";
import { APIPromise } from "../types/async.js";
import { Result } from "../types/fp.js";
/**
 * acknowledgeRiskPolicyChallenge risk
 *
 * @remarks
 * Acknowledge a risk policy warn/challenge from a warning-link token. Records the acknowledgement so the user's retried action proceeds; self-service (no admin approval).
 */
export declare function riskPolicyChallengesAcknowledge(client: GramCore, request: AcknowledgeRiskPolicyChallengeRequest, security?: AcknowledgeRiskPolicyChallengeSecurity | undefined, options?: RequestOptions): APIPromise<Result<AcknowledgeRiskPolicyChallengeResponseBody, ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError>>;
//# sourceMappingURL=riskPolicyChallengesAcknowledge.d.ts.map