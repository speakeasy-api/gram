import * as z from "zod/v4-mini";
import {
  CreateCustomDetectionRuleRequestBody,
  CreateCustomDetectionRuleRequestBody$Outbound,
} from "../components/createcustomdetectionrulerequestbody.js";
export type CreateCustomDetectionRuleSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CreateCustomDetectionRuleSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CreateCustomDetectionRuleSecurity = {
  option1?: CreateCustomDetectionRuleSecurityOption1 | undefined;
  option2?: CreateCustomDetectionRuleSecurityOption2 | undefined;
};
export type CreateCustomDetectionRuleRequest = {
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
  createCustomDetectionRuleRequestBody: CreateCustomDetectionRuleRequestBody;
};
/** @internal */
export type CreateCustomDetectionRuleSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateCustomDetectionRuleSecurityOption1$outboundSchema: z.ZodMiniType<
  CreateCustomDetectionRuleSecurityOption1$Outbound,
  CreateCustomDetectionRuleSecurityOption1
>;
export declare function createCustomDetectionRuleSecurityOption1ToJSON(
  createCustomDetectionRuleSecurityOption1: CreateCustomDetectionRuleSecurityOption1,
): string;
/** @internal */
export type CreateCustomDetectionRuleSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateCustomDetectionRuleSecurityOption2$outboundSchema: z.ZodMiniType<
  CreateCustomDetectionRuleSecurityOption2$Outbound,
  CreateCustomDetectionRuleSecurityOption2
>;
export declare function createCustomDetectionRuleSecurityOption2ToJSON(
  createCustomDetectionRuleSecurityOption2: CreateCustomDetectionRuleSecurityOption2,
): string;
/** @internal */
export type CreateCustomDetectionRuleSecurity$Outbound = {
  Option1?: CreateCustomDetectionRuleSecurityOption1$Outbound | undefined;
  Option2?: CreateCustomDetectionRuleSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateCustomDetectionRuleSecurity$outboundSchema: z.ZodMiniType<
  CreateCustomDetectionRuleSecurity$Outbound,
  CreateCustomDetectionRuleSecurity
>;
export declare function createCustomDetectionRuleSecurityToJSON(
  createCustomDetectionRuleSecurity: CreateCustomDetectionRuleSecurity,
): string;
/** @internal */
export type CreateCustomDetectionRuleRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateCustomDetectionRuleRequestBody: CreateCustomDetectionRuleRequestBody$Outbound;
};
/** @internal */
export declare const CreateCustomDetectionRuleRequest$outboundSchema: z.ZodMiniType<
  CreateCustomDetectionRuleRequest$Outbound,
  CreateCustomDetectionRuleRequest
>;
export declare function createCustomDetectionRuleRequestToJSON(
  createCustomDetectionRuleRequest: CreateCustomDetectionRuleRequest,
): string;
//# sourceMappingURL=createcustomdetectionrule.d.ts.map
