import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The reviewer's ground-truth verdict for this session.
 */
export declare const SaveRiskEvalReviewRequestBodyVerdict: {
    readonly Correct: "correct";
    readonly FalsePositive: "false_positive";
    readonly Missed: "missed";
};
/**
 * The reviewer's ground-truth verdict for this session.
 */
export type SaveRiskEvalReviewRequestBodyVerdict = ClosedEnum<typeof SaveRiskEvalReviewRequestBodyVerdict>;
export type SaveRiskEvalReviewRequestBody = {
    /**
     * The chat session being judged.
     */
    chatId: string;
    /**
     * The prompt-based policy the verdict belongs to.
     */
    policyId: string;
    /**
     * The reviewer's ground-truth verdict for this session.
     */
    verdict: SaveRiskEvalReviewRequestBodyVerdict;
};
/** @internal */
export declare const SaveRiskEvalReviewRequestBodyVerdict$outboundSchema: z.ZodMiniEnum<typeof SaveRiskEvalReviewRequestBodyVerdict>;
/** @internal */
export type SaveRiskEvalReviewRequestBody$Outbound = {
    chat_id: string;
    policy_id: string;
    verdict: string;
};
/** @internal */
export declare const SaveRiskEvalReviewRequestBody$outboundSchema: z.ZodMiniType<SaveRiskEvalReviewRequestBody$Outbound, SaveRiskEvalReviewRequestBody>;
export declare function saveRiskEvalReviewRequestBodyToJSON(saveRiskEvalReviewRequestBody: SaveRiskEvalReviewRequestBody): string;
//# sourceMappingURL=saveriskevalreviewrequestbody.d.ts.map