import * as z from "zod/v4-mini";
export type DeleteRiskExclusionSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DeleteRiskExclusionSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DeleteRiskExclusionSecurity = {
  option1?: DeleteRiskExclusionSecurityOption1 | undefined;
  option2?: DeleteRiskExclusionSecurityOption2 | undefined;
};
export type DeleteRiskExclusionRequest = {
  /**
   * The exclusion ID.
   */
  id: string;
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
export type DeleteRiskExclusionSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteRiskExclusionSecurityOption1$outboundSchema: z.ZodMiniType<
  DeleteRiskExclusionSecurityOption1$Outbound,
  DeleteRiskExclusionSecurityOption1
>;
export declare function deleteRiskExclusionSecurityOption1ToJSON(
  deleteRiskExclusionSecurityOption1: DeleteRiskExclusionSecurityOption1,
): string;
/** @internal */
export type DeleteRiskExclusionSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteRiskExclusionSecurityOption2$outboundSchema: z.ZodMiniType<
  DeleteRiskExclusionSecurityOption2$Outbound,
  DeleteRiskExclusionSecurityOption2
>;
export declare function deleteRiskExclusionSecurityOption2ToJSON(
  deleteRiskExclusionSecurityOption2: DeleteRiskExclusionSecurityOption2,
): string;
/** @internal */
export type DeleteRiskExclusionSecurity$Outbound = {
  Option1?: DeleteRiskExclusionSecurityOption1$Outbound | undefined;
  Option2?: DeleteRiskExclusionSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteRiskExclusionSecurity$outboundSchema: z.ZodMiniType<
  DeleteRiskExclusionSecurity$Outbound,
  DeleteRiskExclusionSecurity
>;
export declare function deleteRiskExclusionSecurityToJSON(
  deleteRiskExclusionSecurity: DeleteRiskExclusionSecurity,
): string;
/** @internal */
export type DeleteRiskExclusionRequest$Outbound = {
  id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteRiskExclusionRequest$outboundSchema: z.ZodMiniType<
  DeleteRiskExclusionRequest$Outbound,
  DeleteRiskExclusionRequest
>;
export declare function deleteRiskExclusionRequestToJSON(
  deleteRiskExclusionRequest: DeleteRiskExclusionRequest,
): string;
//# sourceMappingURL=deleteriskexclusion.d.ts.map
