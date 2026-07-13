import { MutationKey, UseMutationResult } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { AcknowledgeRiskPolicyChallengeResponseBody } from "../models/components/acknowledgeriskpolicychallengeresponsebody.js";
import { GramError } from "../models/errors/gramerror.js";
import { ConnectionError, InvalidRequestError, RequestAbortedError, RequestTimeoutError, UnexpectedClientError } from "../models/errors/httpclienterrors.js";
import { ResponseValidationError } from "../models/errors/responsevalidationerror.js";
import { SDKValidationError } from "../models/errors/sdkvalidationerror.js";
import { ServiceError } from "../models/errors/serviceerror.js";
import { AcknowledgeRiskPolicyChallengeRequest, AcknowledgeRiskPolicyChallengeSecurity } from "../models/operations/acknowledgeriskpolicychallenge.js";
import { MutationHookOptions } from "./_types.js";
export type RiskAcknowledgePolicyChallengeMutationVariables = {
    request: AcknowledgeRiskPolicyChallengeRequest;
    security?: AcknowledgeRiskPolicyChallengeSecurity | undefined;
    options?: RequestOptions;
};
export type RiskAcknowledgePolicyChallengeMutationData = AcknowledgeRiskPolicyChallengeResponseBody;
export type RiskAcknowledgePolicyChallengeMutationError = ServiceError | GramError | ResponseValidationError | ConnectionError | RequestAbortedError | RequestTimeoutError | InvalidRequestError | UnexpectedClientError | SDKValidationError;
/**
 * acknowledgeRiskPolicyChallenge risk
 *
 * @remarks
 * Acknowledge a risk policy warn/challenge from a warning-link token. Records the acknowledgement so the user's retried action proceeds; self-service (no admin approval).
 */
export declare function useRiskAcknowledgePolicyChallengeMutation(options?: MutationHookOptions<RiskAcknowledgePolicyChallengeMutationData, RiskAcknowledgePolicyChallengeMutationError, RiskAcknowledgePolicyChallengeMutationVariables>): UseMutationResult<RiskAcknowledgePolicyChallengeMutationData, RiskAcknowledgePolicyChallengeMutationError, RiskAcknowledgePolicyChallengeMutationVariables>;
export declare function mutationKeyRiskAcknowledgePolicyChallenge(): MutationKey;
export declare function buildRiskAcknowledgePolicyChallengeMutation(client$: GramCore, hookOptions?: RequestOptions): {
    mutationKey: MutationKey;
    mutationFn: (variables: RiskAcknowledgePolicyChallengeMutationVariables) => Promise<RiskAcknowledgePolicyChallengeMutationData>;
};
//# sourceMappingURL=riskAcknowledgePolicyChallenge.d.ts.map