import * as z from "zod/v4-mini";
import {
  UpdateShadowMCPAccessRuleForm,
  UpdateShadowMCPAccessRuleForm$Outbound,
} from "../components/updateshadowmcpaccessruleform.js";
export type UpdateShadowMCPAccessRuleSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateShadowMCPAccessRuleRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  updateShadowMCPAccessRuleForm: UpdateShadowMCPAccessRuleForm;
};
/** @internal */
export type UpdateShadowMCPAccessRuleSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateShadowMCPAccessRuleSecurity$outboundSchema: z.ZodMiniType<
  UpdateShadowMCPAccessRuleSecurity$Outbound,
  UpdateShadowMCPAccessRuleSecurity
>;
export declare function updateShadowMCPAccessRuleSecurityToJSON(
  updateShadowMCPAccessRuleSecurity: UpdateShadowMCPAccessRuleSecurity,
): string;
/** @internal */
export type UpdateShadowMCPAccessRuleRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  UpdateShadowMCPAccessRuleForm: UpdateShadowMCPAccessRuleForm$Outbound;
};
/** @internal */
export declare const UpdateShadowMCPAccessRuleRequest$outboundSchema: z.ZodMiniType<
  UpdateShadowMCPAccessRuleRequest$Outbound,
  UpdateShadowMCPAccessRuleRequest
>;
export declare function updateShadowMCPAccessRuleRequestToJSON(
  updateShadowMCPAccessRuleRequest: UpdateShadowMCPAccessRuleRequest,
): string;
//# sourceMappingURL=updateshadowmcpaccessrule.d.ts.map
