import * as z from "zod/v4-mini";
import {
  TestDetectionRuleRequestBody,
  TestDetectionRuleRequestBody$Outbound,
} from "../components/testdetectionrulerequestbody.js";
export type TestDetectionRuleSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type TestDetectionRuleSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type TestDetectionRuleSecurity = {
  option1?: TestDetectionRuleSecurityOption1 | undefined;
  option2?: TestDetectionRuleSecurityOption2 | undefined;
};
export type TestDetectionRuleRequest = {
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
  testDetectionRuleRequestBody: TestDetectionRuleRequestBody;
};
/** @internal */
export type TestDetectionRuleSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const TestDetectionRuleSecurityOption1$outboundSchema: z.ZodMiniType<
  TestDetectionRuleSecurityOption1$Outbound,
  TestDetectionRuleSecurityOption1
>;
export declare function testDetectionRuleSecurityOption1ToJSON(
  testDetectionRuleSecurityOption1: TestDetectionRuleSecurityOption1,
): string;
/** @internal */
export type TestDetectionRuleSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const TestDetectionRuleSecurityOption2$outboundSchema: z.ZodMiniType<
  TestDetectionRuleSecurityOption2$Outbound,
  TestDetectionRuleSecurityOption2
>;
export declare function testDetectionRuleSecurityOption2ToJSON(
  testDetectionRuleSecurityOption2: TestDetectionRuleSecurityOption2,
): string;
/** @internal */
export type TestDetectionRuleSecurity$Outbound = {
  Option1?: TestDetectionRuleSecurityOption1$Outbound | undefined;
  Option2?: TestDetectionRuleSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const TestDetectionRuleSecurity$outboundSchema: z.ZodMiniType<
  TestDetectionRuleSecurity$Outbound,
  TestDetectionRuleSecurity
>;
export declare function testDetectionRuleSecurityToJSON(
  testDetectionRuleSecurity: TestDetectionRuleSecurity,
): string;
/** @internal */
export type TestDetectionRuleRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  TestDetectionRuleRequestBody: TestDetectionRuleRequestBody$Outbound;
};
/** @internal */
export declare const TestDetectionRuleRequest$outboundSchema: z.ZodMiniType<
  TestDetectionRuleRequest$Outbound,
  TestDetectionRuleRequest
>;
export declare function testDetectionRuleRequestToJSON(
  testDetectionRuleRequest: TestDetectionRuleRequest,
): string;
//# sourceMappingURL=testdetectionrule.d.ts.map
