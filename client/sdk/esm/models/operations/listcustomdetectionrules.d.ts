import * as z from "zod/v4-mini";
export type ListCustomDetectionRulesSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListCustomDetectionRulesSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListCustomDetectionRulesSecurity = {
  option1?: ListCustomDetectionRulesSecurityOption1 | undefined;
  option2?: ListCustomDetectionRulesSecurityOption2 | undefined;
};
export type ListCustomDetectionRulesRequest = {
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
export type ListCustomDetectionRulesSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListCustomDetectionRulesSecurityOption1$outboundSchema: z.ZodMiniType<
  ListCustomDetectionRulesSecurityOption1$Outbound,
  ListCustomDetectionRulesSecurityOption1
>;
export declare function listCustomDetectionRulesSecurityOption1ToJSON(
  listCustomDetectionRulesSecurityOption1: ListCustomDetectionRulesSecurityOption1,
): string;
/** @internal */
export type ListCustomDetectionRulesSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListCustomDetectionRulesSecurityOption2$outboundSchema: z.ZodMiniType<
  ListCustomDetectionRulesSecurityOption2$Outbound,
  ListCustomDetectionRulesSecurityOption2
>;
export declare function listCustomDetectionRulesSecurityOption2ToJSON(
  listCustomDetectionRulesSecurityOption2: ListCustomDetectionRulesSecurityOption2,
): string;
/** @internal */
export type ListCustomDetectionRulesSecurity$Outbound = {
  Option1?: ListCustomDetectionRulesSecurityOption1$Outbound | undefined;
  Option2?: ListCustomDetectionRulesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListCustomDetectionRulesSecurity$outboundSchema: z.ZodMiniType<
  ListCustomDetectionRulesSecurity$Outbound,
  ListCustomDetectionRulesSecurity
>;
export declare function listCustomDetectionRulesSecurityToJSON(
  listCustomDetectionRulesSecurity: ListCustomDetectionRulesSecurity,
): string;
/** @internal */
export type ListCustomDetectionRulesRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListCustomDetectionRulesRequest$outboundSchema: z.ZodMiniType<
  ListCustomDetectionRulesRequest$Outbound,
  ListCustomDetectionRulesRequest
>;
export declare function listCustomDetectionRulesRequestToJSON(
  listCustomDetectionRulesRequest: ListCustomDetectionRulesRequest,
): string;
//# sourceMappingURL=listcustomdetectionrules.d.ts.map
