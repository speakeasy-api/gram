import * as z from "zod/v4-mini";
export type DeleteRiskEvalReviewSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DeleteRiskEvalReviewSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DeleteRiskEvalReviewSecurity = {
  option1?: DeleteRiskEvalReviewSecurityOption1 | undefined;
  option2?: DeleteRiskEvalReviewSecurityOption2 | undefined;
};
export type DeleteRiskEvalReviewRequest = {
  /**
   * The policy the verdict belongs to.
   */
  policyId: string;
  /**
   * The chat session whose verdict to clear.
   */
  chatId: string;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type DeleteRiskEvalReviewSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteRiskEvalReviewSecurityOption1$outboundSchema: z.ZodMiniType<
  DeleteRiskEvalReviewSecurityOption1$Outbound,
  DeleteRiskEvalReviewSecurityOption1
>;
export declare function deleteRiskEvalReviewSecurityOption1ToJSON(
  deleteRiskEvalReviewSecurityOption1: DeleteRiskEvalReviewSecurityOption1,
): string;
/** @internal */
export type DeleteRiskEvalReviewSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteRiskEvalReviewSecurityOption2$outboundSchema: z.ZodMiniType<
  DeleteRiskEvalReviewSecurityOption2$Outbound,
  DeleteRiskEvalReviewSecurityOption2
>;
export declare function deleteRiskEvalReviewSecurityOption2ToJSON(
  deleteRiskEvalReviewSecurityOption2: DeleteRiskEvalReviewSecurityOption2,
): string;
/** @internal */
export type DeleteRiskEvalReviewSecurity$Outbound = {
  Option1?: DeleteRiskEvalReviewSecurityOption1$Outbound | undefined;
  Option2?: DeleteRiskEvalReviewSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteRiskEvalReviewSecurity$outboundSchema: z.ZodMiniType<
  DeleteRiskEvalReviewSecurity$Outbound,
  DeleteRiskEvalReviewSecurity
>;
export declare function deleteRiskEvalReviewSecurityToJSON(
  deleteRiskEvalReviewSecurity: DeleteRiskEvalReviewSecurity,
): string;
/** @internal */
export type DeleteRiskEvalReviewRequest$Outbound = {
  policy_id: string;
  chat_id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteRiskEvalReviewRequest$outboundSchema: z.ZodMiniType<
  DeleteRiskEvalReviewRequest$Outbound,
  DeleteRiskEvalReviewRequest
>;
export declare function deleteRiskEvalReviewRequestToJSON(
  deleteRiskEvalReviewRequest: DeleteRiskEvalReviewRequest,
): string;
//# sourceMappingURL=deleteriskevalreview.d.ts.map
