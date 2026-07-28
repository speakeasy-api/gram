import * as z from "zod/v4-mini";
import {
  CreateShadowMCPAccessRuleForm,
  CreateShadowMCPAccessRuleForm$Outbound,
} from "../components/createshadowmcpaccessruleform.js";
export type CreateShadowMCPAccessRuleSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreateShadowMCPAccessRuleRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createShadowMCPAccessRuleForm: CreateShadowMCPAccessRuleForm;
};
/** @internal */
export type CreateShadowMCPAccessRuleSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateShadowMCPAccessRuleSecurity$outboundSchema: z.ZodMiniType<
  CreateShadowMCPAccessRuleSecurity$Outbound,
  CreateShadowMCPAccessRuleSecurity
>;
export declare function createShadowMCPAccessRuleSecurityToJSON(
  createShadowMCPAccessRuleSecurity: CreateShadowMCPAccessRuleSecurity,
): string;
/** @internal */
export type CreateShadowMCPAccessRuleRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  CreateShadowMCPAccessRuleForm: CreateShadowMCPAccessRuleForm$Outbound;
};
/** @internal */
export declare const CreateShadowMCPAccessRuleRequest$outboundSchema: z.ZodMiniType<
  CreateShadowMCPAccessRuleRequest$Outbound,
  CreateShadowMCPAccessRuleRequest
>;
export declare function createShadowMCPAccessRuleRequestToJSON(
  createShadowMCPAccessRuleRequest: CreateShadowMCPAccessRuleRequest,
): string;
//# sourceMappingURL=createshadowmcpaccessrule.d.ts.map
