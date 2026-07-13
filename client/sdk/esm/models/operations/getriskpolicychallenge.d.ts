import * as z from "zod/v4-mini";
import { AcknowledgeRiskPolicyChallengeRequestBody, AcknowledgeRiskPolicyChallengeRequestBody$Outbound } from "../components/acknowledgeriskpolicychallengerequestbody.js";
export type GetRiskPolicyChallengeSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type GetRiskPolicyChallengeRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    acknowledgeRiskPolicyChallengeRequestBody: AcknowledgeRiskPolicyChallengeRequestBody;
};
/** @internal */
export type GetRiskPolicyChallengeSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetRiskPolicyChallengeSecurity$outboundSchema: z.ZodMiniType<GetRiskPolicyChallengeSecurity$Outbound, GetRiskPolicyChallengeSecurity>;
export declare function getRiskPolicyChallengeSecurityToJSON(getRiskPolicyChallengeSecurity: GetRiskPolicyChallengeSecurity): string;
/** @internal */
export type GetRiskPolicyChallengeRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    AcknowledgeRiskPolicyChallengeRequestBody: AcknowledgeRiskPolicyChallengeRequestBody$Outbound;
};
/** @internal */
export declare const GetRiskPolicyChallengeRequest$outboundSchema: z.ZodMiniType<GetRiskPolicyChallengeRequest$Outbound, GetRiskPolicyChallengeRequest>;
export declare function getRiskPolicyChallengeRequestToJSON(getRiskPolicyChallengeRequest: GetRiskPolicyChallengeRequest): string;
//# sourceMappingURL=getriskpolicychallenge.d.ts.map