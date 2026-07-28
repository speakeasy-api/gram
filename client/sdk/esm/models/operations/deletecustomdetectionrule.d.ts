import * as z from "zod/v4-mini";
import {
  RiskIDRequestBody,
  RiskIDRequestBody$Outbound,
} from "../components/riskidrequestbody.js";
export type DeleteCustomDetectionRuleSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DeleteCustomDetectionRuleSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DeleteCustomDetectionRuleSecurity = {
  option1?: DeleteCustomDetectionRuleSecurityOption1 | undefined;
  option2?: DeleteCustomDetectionRuleSecurityOption2 | undefined;
};
export type DeleteCustomDetectionRuleRequest = {
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
  riskIDRequestBody: RiskIDRequestBody;
};
/** @internal */
export type DeleteCustomDetectionRuleSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteCustomDetectionRuleSecurityOption1$outboundSchema: z.ZodMiniType<
  DeleteCustomDetectionRuleSecurityOption1$Outbound,
  DeleteCustomDetectionRuleSecurityOption1
>;
export declare function deleteCustomDetectionRuleSecurityOption1ToJSON(
  deleteCustomDetectionRuleSecurityOption1: DeleteCustomDetectionRuleSecurityOption1,
): string;
/** @internal */
export type DeleteCustomDetectionRuleSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteCustomDetectionRuleSecurityOption2$outboundSchema: z.ZodMiniType<
  DeleteCustomDetectionRuleSecurityOption2$Outbound,
  DeleteCustomDetectionRuleSecurityOption2
>;
export declare function deleteCustomDetectionRuleSecurityOption2ToJSON(
  deleteCustomDetectionRuleSecurityOption2: DeleteCustomDetectionRuleSecurityOption2,
): string;
/** @internal */
export type DeleteCustomDetectionRuleSecurity$Outbound = {
  Option1?: DeleteCustomDetectionRuleSecurityOption1$Outbound | undefined;
  Option2?: DeleteCustomDetectionRuleSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteCustomDetectionRuleSecurity$outboundSchema: z.ZodMiniType<
  DeleteCustomDetectionRuleSecurity$Outbound,
  DeleteCustomDetectionRuleSecurity
>;
export declare function deleteCustomDetectionRuleSecurityToJSON(
  deleteCustomDetectionRuleSecurity: DeleteCustomDetectionRuleSecurity,
): string;
/** @internal */
export type DeleteCustomDetectionRuleRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  RiskIDRequestBody: RiskIDRequestBody$Outbound;
};
/** @internal */
export declare const DeleteCustomDetectionRuleRequest$outboundSchema: z.ZodMiniType<
  DeleteCustomDetectionRuleRequest$Outbound,
  DeleteCustomDetectionRuleRequest
>;
export declare function deleteCustomDetectionRuleRequestToJSON(
  deleteCustomDetectionRuleRequest: DeleteCustomDetectionRuleRequest,
): string;
//# sourceMappingURL=deletecustomdetectionrule.d.ts.map
