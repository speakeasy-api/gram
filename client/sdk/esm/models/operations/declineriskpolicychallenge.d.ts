import * as z from "zod/v4-mini";
import { AcknowledgeRiskPolicyChallengeRequestBody, AcknowledgeRiskPolicyChallengeRequestBody$Outbound } from "../components/acknowledgeriskpolicychallengerequestbody.js";
export type DeclineRiskPolicyChallengeSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type DeclineRiskPolicyChallengeRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    acknowledgeRiskPolicyChallengeRequestBody: AcknowledgeRiskPolicyChallengeRequestBody;
};
/** @internal */
export type DeclineRiskPolicyChallengeSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeclineRiskPolicyChallengeSecurity$outboundSchema: z.ZodMiniType<DeclineRiskPolicyChallengeSecurity$Outbound, DeclineRiskPolicyChallengeSecurity>;
export declare function declineRiskPolicyChallengeSecurityToJSON(declineRiskPolicyChallengeSecurity: DeclineRiskPolicyChallengeSecurity): string;
/** @internal */
export type DeclineRiskPolicyChallengeRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    AcknowledgeRiskPolicyChallengeRequestBody: AcknowledgeRiskPolicyChallengeRequestBody$Outbound;
};
/** @internal */
export declare const DeclineRiskPolicyChallengeRequest$outboundSchema: z.ZodMiniType<DeclineRiskPolicyChallengeRequest$Outbound, DeclineRiskPolicyChallengeRequest>;
export declare function declineRiskPolicyChallengeRequestToJSON(declineRiskPolicyChallengeRequest: DeclineRiskPolicyChallengeRequest): string;
//# sourceMappingURL=declineriskpolicychallenge.d.ts.map