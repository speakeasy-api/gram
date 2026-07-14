import * as z from "zod/v4-mini";
import {
  SuggestCustomDetectionRuleRequestBody,
  SuggestCustomDetectionRuleRequestBody$Outbound,
} from "../components/suggestcustomdetectionrulerequestbody.js";
export type SuggestCustomDetectionRuleSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type SuggestCustomDetectionRuleSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type SuggestCustomDetectionRuleSecurity = {
  option1?: SuggestCustomDetectionRuleSecurityOption1 | undefined;
  option2?: SuggestCustomDetectionRuleSecurityOption2 | undefined;
};
export type SuggestCustomDetectionRuleRequest = {
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
  suggestCustomDetectionRuleRequestBody: SuggestCustomDetectionRuleRequestBody;
};
/** @internal */
export type SuggestCustomDetectionRuleSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SuggestCustomDetectionRuleSecurityOption1$outboundSchema: z.ZodMiniType<
  SuggestCustomDetectionRuleSecurityOption1$Outbound,
  SuggestCustomDetectionRuleSecurityOption1
>;
export declare function suggestCustomDetectionRuleSecurityOption1ToJSON(
  suggestCustomDetectionRuleSecurityOption1: SuggestCustomDetectionRuleSecurityOption1,
): string;
/** @internal */
export type SuggestCustomDetectionRuleSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const SuggestCustomDetectionRuleSecurityOption2$outboundSchema: z.ZodMiniType<
  SuggestCustomDetectionRuleSecurityOption2$Outbound,
  SuggestCustomDetectionRuleSecurityOption2
>;
export declare function suggestCustomDetectionRuleSecurityOption2ToJSON(
  suggestCustomDetectionRuleSecurityOption2: SuggestCustomDetectionRuleSecurityOption2,
): string;
/** @internal */
export type SuggestCustomDetectionRuleSecurity$Outbound = {
  Option1?: SuggestCustomDetectionRuleSecurityOption1$Outbound | undefined;
  Option2?: SuggestCustomDetectionRuleSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SuggestCustomDetectionRuleSecurity$outboundSchema: z.ZodMiniType<
  SuggestCustomDetectionRuleSecurity$Outbound,
  SuggestCustomDetectionRuleSecurity
>;
export declare function suggestCustomDetectionRuleSecurityToJSON(
  suggestCustomDetectionRuleSecurity: SuggestCustomDetectionRuleSecurity,
): string;
/** @internal */
export type SuggestCustomDetectionRuleRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  SuggestCustomDetectionRuleRequestBody: SuggestCustomDetectionRuleRequestBody$Outbound;
};
/** @internal */
export declare const SuggestCustomDetectionRuleRequest$outboundSchema: z.ZodMiniType<
  SuggestCustomDetectionRuleRequest$Outbound,
  SuggestCustomDetectionRuleRequest
>;
export declare function suggestCustomDetectionRuleRequestToJSON(
  suggestCustomDetectionRuleRequest: SuggestCustomDetectionRuleRequest,
): string;
//# sourceMappingURL=suggestcustomdetectionrule.d.ts.map
