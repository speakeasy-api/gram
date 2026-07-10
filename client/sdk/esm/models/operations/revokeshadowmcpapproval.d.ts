import * as z from "zod/v4-mini";
export type RevokeShadowMCPApprovalSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type RevokeShadowMCPApprovalSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type RevokeShadowMCPApprovalSecurity = {
  option1?: RevokeShadowMCPApprovalSecurityOption1 | undefined;
  option2?: RevokeShadowMCPApprovalSecurityOption2 | undefined;
};
export type RevokeShadowMCPApprovalRequest = {
  /**
   * The risk policy ID.
   */
  policyId: string;
  /**
   * The MCP server identifier to revoke — exactly the value used to approve.
   */
  match: string;
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
export type RevokeShadowMCPApprovalSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RevokeShadowMCPApprovalSecurityOption1$outboundSchema: z.ZodMiniType<
  RevokeShadowMCPApprovalSecurityOption1$Outbound,
  RevokeShadowMCPApprovalSecurityOption1
>;
export declare function revokeShadowMCPApprovalSecurityOption1ToJSON(
  revokeShadowMCPApprovalSecurityOption1: RevokeShadowMCPApprovalSecurityOption1,
): string;
/** @internal */
export type RevokeShadowMCPApprovalSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const RevokeShadowMCPApprovalSecurityOption2$outboundSchema: z.ZodMiniType<
  RevokeShadowMCPApprovalSecurityOption2$Outbound,
  RevokeShadowMCPApprovalSecurityOption2
>;
export declare function revokeShadowMCPApprovalSecurityOption2ToJSON(
  revokeShadowMCPApprovalSecurityOption2: RevokeShadowMCPApprovalSecurityOption2,
): string;
/** @internal */
export type RevokeShadowMCPApprovalSecurity$Outbound = {
  Option1?: RevokeShadowMCPApprovalSecurityOption1$Outbound | undefined;
  Option2?: RevokeShadowMCPApprovalSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RevokeShadowMCPApprovalSecurity$outboundSchema: z.ZodMiniType<
  RevokeShadowMCPApprovalSecurity$Outbound,
  RevokeShadowMCPApprovalSecurity
>;
export declare function revokeShadowMCPApprovalSecurityToJSON(
  revokeShadowMCPApprovalSecurity: RevokeShadowMCPApprovalSecurity,
): string;
/** @internal */
export type RevokeShadowMCPApprovalRequest$Outbound = {
  policy_id: string;
  match: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const RevokeShadowMCPApprovalRequest$outboundSchema: z.ZodMiniType<
  RevokeShadowMCPApprovalRequest$Outbound,
  RevokeShadowMCPApprovalRequest
>;
export declare function revokeShadowMCPApprovalRequestToJSON(
  revokeShadowMCPApprovalRequest: RevokeShadowMCPApprovalRequest,
): string;
//# sourceMappingURL=revokeshadowmcpapproval.d.ts.map
