import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { DeclineRiskPolicyChallengeResponseBody } from "../models/components/declineriskpolicychallengeresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { DeclineRiskPolicyChallengeRequest, DeclineRiskPolicyChallengeSecurity } from "../models/operations/declineriskpolicychallenge.js";
import { MutationHookOptions } from "./_types.js";
export type RiskDeclinePolicyChallengeMutationVariables = {
    request: DeclineRiskPolicyChallengeRequest;
    security?: DeclineRiskPolicyChallengeSecurity | undefined;
    options?: RequestOptions;
};
export type RiskDeclinePolicyChallengeMutationData = DeclineRiskPolicyChallengeResponseBody;
export type RiskDeclinePolicyChallengeMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * declineRiskPolicyChallenge risk
 *
 * @remarks
 * Decline a risk policy warn/challenge from a warning-link token: invalidate the link and mark the challenge declined. The blocked action stays blocked.
 */
export declare function useRiskDeclinePolicyChallengeMutation(options?: MutationHookOptions<RiskDeclinePolicyChallengeMutationData, RiskDeclinePolicyChallengeMutationError, RiskDeclinePolicyChallengeMutationVariables>): UseMutationResult<RiskDeclinePolicyChallengeMutationData, RiskDeclinePolicyChallengeMutationError, RiskDeclinePolicyChallengeMutationVariables>;
export declare function mutationKeyRiskDeclinePolicyChallenge(): MutationKey;
export declare function buildRiskDeclinePolicyChallengeMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskDeclinePolicyChallengeMutationVariables) => Promise<RiskDeclinePolicyChallengeMutationData>;
};
//# sourceMappingURL=riskDeclinePolicyChallenge.d.ts.map