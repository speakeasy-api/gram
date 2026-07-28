import * as z from "zod/v4-mini";
export type DeleteRiskPolicySecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DeleteRiskPolicySecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DeleteRiskPolicySecurity = {
  option1?: DeleteRiskPolicySecurityOption1 | undefined;
  option2?: DeleteRiskPolicySecurityOption2 | undefined;
};
export type DeleteRiskPolicyRequest = {
  /**
   * The policy ID.
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
export type DeleteRiskPolicySecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteRiskPolicySecurityOption1$outboundSchema: z.ZodMiniType<
  DeleteRiskPolicySecurityOption1$Outbound,
  DeleteRiskPolicySecurityOption1
>;
export declare function deleteRiskPolicySecurityOption1ToJSON(
  deleteRiskPolicySecurityOption1: DeleteRiskPolicySecurityOption1,
): string;
/** @internal */
export type DeleteRiskPolicySecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteRiskPolicySecurityOption2$outboundSchema: z.ZodMiniType<
  DeleteRiskPolicySecurityOption2$Outbound,
  DeleteRiskPolicySecurityOption2
>;
export declare function deleteRiskPolicySecurityOption2ToJSON(
  deleteRiskPolicySecurityOption2: DeleteRiskPolicySecurityOption2,
): string;
/** @internal */
export type DeleteRiskPolicySecurity$Outbound = {
  Option1?: DeleteRiskPolicySecurityOption1$Outbound | undefined;
  Option2?: DeleteRiskPolicySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteRiskPolicySecurity$outboundSchema: z.ZodMiniType<
  DeleteRiskPolicySecurity$Outbound,
  DeleteRiskPolicySecurity
>;
export declare function deleteRiskPolicySecurityToJSON(
  deleteRiskPolicySecurity: DeleteRiskPolicySecurity,
): string;
/** @internal */
export type DeleteRiskPolicyRequest$Outbound = {
  id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteRiskPolicyRequest$outboundSchema: z.ZodMiniType<
  DeleteRiskPolicyRequest$Outbound,
  DeleteRiskPolicyRequest
>;
export declare function deleteRiskPolicyRequestToJSON(
  deleteRiskPolicyRequest: DeleteRiskPolicyRequest,
): string;
//# sourceMappingURL=deleteriskpolicy.d.ts.map
