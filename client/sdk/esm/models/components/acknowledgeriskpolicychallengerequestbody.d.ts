import * as z from "zod/v4-mini";
export type AcknowledgeRiskPolicyChallengeRequestBody = {
    /**
     * Acknowledgement token generated when a warn policy challenged the action.
     */
    ackToken: string;
};
/** @internal */
export type AcknowledgeRiskPolicyChallengeRequestBody$Outbound = {
    ack_token: string;
};
/** @internal */
export declare const AcknowledgeRiskPolicyChallengeRequestBody$outboundSchema: z.ZodMiniType<AcknowledgeRiskPolicyChallengeRequestBody$Outbound, AcknowledgeRiskPolicyChallengeRequestBody>;
export declare function acknowledgeRiskPolicyChallengeRequestBodyToJSON(acknowledgeRiskPolicyChallengeRequestBody: AcknowledgeRiskPolicyChallengeRequestBody): string;
//# sourceMappingURL=acknowledgeriskpolicychallengerequestbody.d.ts.map