import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { AcknowledgeRiskPolicyChallengeResponseBody } from "../models/components/acknowledgeriskpolicychallengeresponsebody.js";
import { DeclineRiskPolicyChallengeResponseBody } from "../models/components/declineriskpolicychallengeresponsebody.js";
import { GetRiskPolicyChallengeResponseBody } from "../models/components/getriskpolicychallengeresponsebody.js";
import { AcknowledgeRiskPolicyChallengeRequest, AcknowledgeRiskPolicyChallengeSecurity } from "../models/operations/acknowledgeriskpolicychallenge.js";
import { DeclineRiskPolicyChallengeRequest, DeclineRiskPolicyChallengeSecurity } from "../models/operations/declineriskpolicychallenge.js";
import { GetRiskPolicyChallengeRequest, GetRiskPolicyChallengeSecurity } from "../models/operations/getriskpolicychallenge.js";
export declare class PolicyChallenges extends ClientSDK {
    /**
     * acknowledgeRiskPolicyChallenge risk
     *
     * @remarks
     * Acknowledge a risk policy warn/challenge from a warning-link token. Records the acknowledgement so the user's retried action proceeds; self-service (no admin approval).
     */
    acknowledge(request: AcknowledgeRiskPolicyChallengeRequest, security?: AcknowledgeRiskPolicyChallengeSecurity | undefined, options?: RequestOptions): Promise<AcknowledgeRiskPolicyChallengeResponseBody>;
    /**
     * declineRiskPolicyChallenge risk
     *
     * @remarks
     * Decline a risk policy warn/challenge from a warning-link token: invalidate the link and mark the challenge declined. The blocked action stays blocked.
     */
    decline(request: DeclineRiskPolicyChallengeRequest, security?: DeclineRiskPolicyChallengeSecurity | undefined, options?: RequestOptions): Promise<DeclineRiskPolicyChallengeResponseBody>;
    /**
     * getRiskPolicyChallenge risk
     *
     * @remarks
     * Fetch the details of a risk policy warn/challenge from a warning-link token, WITHOUT acknowledging it. Powers the approval page (shows what was flagged and Approve/Deny actions).
     */
    get(request: GetRiskPolicyChallengeRequest, security?: GetRiskPolicyChallengeSecurity | undefined, options?: RequestOptions): Promise<GetRiskPolicyChallengeResponseBody>;
}
//# sourceMappingURL=policychallenges.d.ts.map