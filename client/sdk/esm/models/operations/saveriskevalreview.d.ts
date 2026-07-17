import * as z from "zod/v4-mini";
import {
  SaveRiskEvalReviewRequestBody,
  SaveRiskEvalReviewRequestBody$Outbound,
} from "../components/saveriskevalreviewrequestbody.js";
export type SaveRiskEvalReviewSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type SaveRiskEvalReviewSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type SaveRiskEvalReviewSecurity = {
  option1?: SaveRiskEvalReviewSecurityOption1 | undefined;
  option2?: SaveRiskEvalReviewSecurityOption2 | undefined;
};
export type SaveRiskEvalReviewRequest = {
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
  saveRiskEvalReviewRequestBody: SaveRiskEvalReviewRequestBody;
};
/** @internal */
export type SaveRiskEvalReviewSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SaveRiskEvalReviewSecurityOption1$outboundSchema: z.ZodMiniType<
  SaveRiskEvalReviewSecurityOption1$Outbound,
  SaveRiskEvalReviewSecurityOption1
>;
export declare function saveRiskEvalReviewSecurityOption1ToJSON(
  saveRiskEvalReviewSecurityOption1: SaveRiskEvalReviewSecurityOption1,
): string;
/** @internal */
export type SaveRiskEvalReviewSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const SaveRiskEvalReviewSecurityOption2$outboundSchema: z.ZodMiniType<
  SaveRiskEvalReviewSecurityOption2$Outbound,
  SaveRiskEvalReviewSecurityOption2
>;
export declare function saveRiskEvalReviewSecurityOption2ToJSON(
  saveRiskEvalReviewSecurityOption2: SaveRiskEvalReviewSecurityOption2,
): string;
/** @internal */
export type SaveRiskEvalReviewSecurity$Outbound = {
  Option1?: SaveRiskEvalReviewSecurityOption1$Outbound | undefined;
  Option2?: SaveRiskEvalReviewSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SaveRiskEvalReviewSecurity$outboundSchema: z.ZodMiniType<
  SaveRiskEvalReviewSecurity$Outbound,
  SaveRiskEvalReviewSecurity
>;
export declare function saveRiskEvalReviewSecurityToJSON(
  saveRiskEvalReviewSecurity: SaveRiskEvalReviewSecurity,
): string;
/** @internal */
export type SaveRiskEvalReviewRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  SaveRiskEvalReviewRequestBody: SaveRiskEvalReviewRequestBody$Outbound;
};
/** @internal */
export declare const SaveRiskEvalReviewRequest$outboundSchema: z.ZodMiniType<
  SaveRiskEvalReviewRequest$Outbound,
  SaveRiskEvalReviewRequest
>;
export declare function saveRiskEvalReviewRequestToJSON(
  saveRiskEvalReviewRequest: SaveRiskEvalReviewRequest,
): string;
//# sourceMappingURL=saveriskevalreview.d.ts.map
