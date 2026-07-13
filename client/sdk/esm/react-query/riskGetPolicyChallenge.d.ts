import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetRiskPolicyChallengeResponseBody } from "../models/components/getriskpolicychallengeresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { GetRiskPolicyChallengeRequest, GetRiskPolicyChallengeSecurity } from "../models/operations/getriskpolicychallenge.js";
import { MutationHookOptions } from "./_types.js";
export type RiskGetPolicyChallengeMutationVariables = {
    request: GetRiskPolicyChallengeRequest;
    security?: GetRiskPolicyChallengeSecurity | undefined;
    options?: RequestOptions;
};
export type RiskGetPolicyChallengeMutationData = GetRiskPolicyChallengeResponseBody;
export type RiskGetPolicyChallengeMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * getRiskPolicyChallenge risk
 *
 * @remarks
 * Fetch the details of a risk policy warn/challenge from a warning-link token, WITHOUT acknowledging it. Powers the approval page (shows what was flagged and Approve/Deny actions).
 */
export declare function useRiskGetPolicyChallengeMutation(options?: MutationHookOptions<RiskGetPolicyChallengeMutationData, RiskGetPolicyChallengeMutationError, RiskGetPolicyChallengeMutationVariables>): UseMutationResult<RiskGetPolicyChallengeMutationData, RiskGetPolicyChallengeMutationError, RiskGetPolicyChallengeMutationVariables>;
export declare function mutationKeyRiskGetPolicyChallenge(): MutationKey;
export declare function buildRiskGetPolicyChallengeMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskGetPolicyChallengeMutationVariables) => Promise<RiskGetPolicyChallengeMutationData>;
};
//# sourceMappingURL=riskGetPolicyChallenge.d.ts.map