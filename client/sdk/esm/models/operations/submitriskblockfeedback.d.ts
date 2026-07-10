import * as z from "zod/v4-mini";
import { SubmitRiskBlockFeedbackRequestBody, SubmitRiskBlockFeedbackRequestBody$Outbound } from "../components/submitriskblockfeedbackrequestbody.js";
export type SubmitRiskBlockFeedbackSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type SubmitRiskBlockFeedbackRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    submitRiskBlockFeedbackRequestBody: SubmitRiskBlockFeedbackRequestBody;
};
/** @internal */
export type SubmitRiskBlockFeedbackSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const SubmitRiskBlockFeedbackSecurity$outboundSchema: z.ZodMiniType<SubmitRiskBlockFeedbackSecurity$Outbound, SubmitRiskBlockFeedbackSecurity>;
export declare function submitRiskBlockFeedbackSecurityToJSON(submitRiskBlockFeedbackSecurity: SubmitRiskBlockFeedbackSecurity): string;
/** @internal */
export type SubmitRiskBlockFeedbackRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    SubmitRiskBlockFeedbackRequestBody: SubmitRiskBlockFeedbackRequestBody$Outbound;
};
/** @internal */
export declare const SubmitRiskBlockFeedbackRequest$outboundSchema: z.ZodMiniType<SubmitRiskBlockFeedbackRequest$Outbound, SubmitRiskBlockFeedbackRequest>;
export declare function submitRiskBlockFeedbackRequestToJSON(submitRiskBlockFeedbackRequest: SubmitRiskBlockFeedbackRequest): string;
//# sourceMappingURL=submitriskblockfeedback.d.ts.map