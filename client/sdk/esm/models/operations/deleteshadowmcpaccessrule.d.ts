import * as z from "zod/v4-mini";
export type DeleteShadowMCPAccessRuleSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteShadowMCPAccessRuleRequest = {
  id: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type DeleteShadowMCPAccessRuleSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteShadowMCPAccessRuleSecurity$outboundSchema: z.ZodMiniType<
  DeleteShadowMCPAccessRuleSecurity$Outbound,
  DeleteShadowMCPAccessRuleSecurity
>;
export declare function deleteShadowMCPAccessRuleSecurityToJSON(
  deleteShadowMCPAccessRuleSecurity: DeleteShadowMCPAccessRuleSecurity,
): string;
/** @internal */
export type DeleteShadowMCPAccessRuleRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteShadowMCPAccessRuleRequest$outboundSchema: z.ZodMiniType<
  DeleteShadowMCPAccessRuleRequest$Outbound,
  DeleteShadowMCPAccessRuleRequest
>;
export declare function deleteShadowMCPAccessRuleRequestToJSON(
  deleteShadowMCPAccessRuleRequest: DeleteShadowMCPAccessRuleRequest,
): string;
//# sourceMappingURL=deleteshadowmcpaccessrule.d.ts.map
