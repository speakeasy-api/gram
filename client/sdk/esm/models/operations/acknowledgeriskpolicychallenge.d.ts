import * as z from "zod/v4-mini";
import { AcknowledgeRiskPolicyChallengeRequestBody, AcknowledgeRiskPolicyChallengeRequestBody$Outbound } from "../components/acknowledgeriskpolicychallengerequestbody.js";
export type AcknowledgeRiskPolicyChallengeSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type AcknowledgeRiskPolicyChallengeRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    acknowledgeRiskPolicyChallengeRequestBody: AcknowledgeRiskPolicyChallengeRequestBody;
};
/** @internal */
export type AcknowledgeRiskPolicyChallengeSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const AcknowledgeRiskPolicyChallengeSecurity$outboundSchema: z.ZodMiniType<AcknowledgeRiskPolicyChallengeSecurity$Outbound, AcknowledgeRiskPolicyChallengeSecurity>;
export declare function acknowledgeRiskPolicyChallengeSecurityToJSON(acknowledgeRiskPolicyChallengeSecurity: AcknowledgeRiskPolicyChallengeSecurity): string;
/** @internal */
export type AcknowledgeRiskPolicyChallengeRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    AcknowledgeRiskPolicyChallengeRequestBody: AcknowledgeRiskPolicyChallengeRequestBody$Outbound;
};
/** @internal */
export declare const AcknowledgeRiskPolicyChallengeRequest$outboundSchema: z.ZodMiniType<AcknowledgeRiskPolicyChallengeRequest$Outbound, AcknowledgeRiskPolicyChallengeRequest>;
export declare function acknowledgeRiskPolicyChallengeRequestToJSON(acknowledgeRiskPolicyChallengeRequest: AcknowledgeRiskPolicyChallengeRequest): string;
//# sourceMappingURL=acknowledgeriskpolicychallenge.d.ts.map