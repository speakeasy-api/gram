import * as z from "zod/v4-mini";
export type GetTemplateSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetTemplateSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetTemplateSecurityOption3 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetTemplateSecurity = {
  option1?: GetTemplateSecurityOption1 | undefined;
  option2?: GetTemplateSecurityOption2 | undefined;
  option3?: GetTemplateSecurityOption3 | undefined;
};
export type GetTemplateRequest = {
  /**
   * The ID of the prompt template
   */
  id?: string | undefined;
  /**
   * The name of the prompt template
   */
  name?: string | undefined;
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
export type GetTemplateSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetTemplateSecurityOption1$outboundSchema: z.ZodMiniType<
  GetTemplateSecurityOption1$Outbound,
  GetTemplateSecurityOption1
>;
export declare function getTemplateSecurityOption1ToJSON(
  getTemplateSecurityOption1: GetTemplateSecurityOption1,
): string;
/** @internal */
export type GetTemplateSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetTemplateSecurityOption2$outboundSchema: z.ZodMiniType<
  GetTemplateSecurityOption2$Outbound,
  GetTemplateSecurityOption2
>;
export declare function getTemplateSecurityOption2ToJSON(
  getTemplateSecurityOption2: GetTemplateSecurityOption2,
): string;
/** @internal */
export type GetTemplateSecurityOption3$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetTemplateSecurityOption3$outboundSchema: z.ZodMiniType<
  GetTemplateSecurityOption3$Outbound,
  GetTemplateSecurityOption3
>;
export declare function getTemplateSecurityOption3ToJSON(
  getTemplateSecurityOption3: GetTemplateSecurityOption3,
): string;
/** @internal */
export type GetTemplateSecurity$Outbound = {
  Option1?: GetTemplateSecurityOption1$Outbound | undefined;
  Option2?: GetTemplateSecurityOption2$Outbound | undefined;
  Option3?: GetTemplateSecurityOption3$Outbound | undefined;
};
/** @internal */
export declare const GetTemplateSecurity$outboundSchema: z.ZodMiniType<
  GetTemplateSecurity$Outbound,
  GetTemplateSecurity
>;
export declare function getTemplateSecurityToJSON(
  getTemplateSecurity: GetTemplateSecurity,
): string;
/** @internal */
export type GetTemplateRequest$Outbound = {
  id?: string | undefined;
  name?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetTemplateRequest$outboundSchema: z.ZodMiniType<
  GetTemplateRequest$Outbound,
  GetTemplateRequest
>;
export declare function getTemplateRequestToJSON(
  getTemplateRequest: GetTemplateRequest,
): string;
//# sourceMappingURL=gettemplate.d.ts.map
