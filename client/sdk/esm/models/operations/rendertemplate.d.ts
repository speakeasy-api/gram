import * as z from "zod/v4-mini";
import {
  RenderTemplateRequestBody,
  RenderTemplateRequestBody$Outbound,
} from "../components/rendertemplaterequestbody.js";
export type RenderTemplateSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type RenderTemplateSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type RenderTemplateSecurityOption3 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type RenderTemplateSecurity = {
  option1?: RenderTemplateSecurityOption1 | undefined;
  option2?: RenderTemplateSecurityOption2 | undefined;
  option3?: RenderTemplateSecurityOption3 | undefined;
};
export type RenderTemplateRequest = {
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
  renderTemplateRequestBody: RenderTemplateRequestBody;
};
/** @internal */
export type RenderTemplateSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const RenderTemplateSecurityOption1$outboundSchema: z.ZodMiniType<
  RenderTemplateSecurityOption1$Outbound,
  RenderTemplateSecurityOption1
>;
export declare function renderTemplateSecurityOption1ToJSON(
  renderTemplateSecurityOption1: RenderTemplateSecurityOption1,
): string;
/** @internal */
export type RenderTemplateSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RenderTemplateSecurityOption2$outboundSchema: z.ZodMiniType<
  RenderTemplateSecurityOption2$Outbound,
  RenderTemplateSecurityOption2
>;
export declare function renderTemplateSecurityOption2ToJSON(
  renderTemplateSecurityOption2: RenderTemplateSecurityOption2,
): string;
/** @internal */
export type RenderTemplateSecurityOption3$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RenderTemplateSecurityOption3$outboundSchema: z.ZodMiniType<
  RenderTemplateSecurityOption3$Outbound,
  RenderTemplateSecurityOption3
>;
export declare function renderTemplateSecurityOption3ToJSON(
  renderTemplateSecurityOption3: RenderTemplateSecurityOption3,
): string;
/** @internal */
export type RenderTemplateSecurity$Outbound = {
  Option1?: RenderTemplateSecurityOption1$Outbound | undefined;
  Option2?: RenderTemplateSecurityOption2$Outbound | undefined;
  Option3?: RenderTemplateSecurityOption3$Outbound | undefined;
};
/** @internal */
export declare const RenderTemplateSecurity$outboundSchema: z.ZodMiniType<
  RenderTemplateSecurity$Outbound,
  RenderTemplateSecurity
>;
export declare function renderTemplateSecurityToJSON(
  renderTemplateSecurity: RenderTemplateSecurity,
): string;
/** @internal */
export type RenderTemplateRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  RenderTemplateRequestBody: RenderTemplateRequestBody$Outbound;
};
/** @internal */
export declare const RenderTemplateRequest$outboundSchema: z.ZodMiniType<
  RenderTemplateRequest$Outbound,
  RenderTemplateRequest
>;
export declare function renderTemplateRequestToJSON(
  renderTemplateRequest: RenderTemplateRequest,
): string;
//# sourceMappingURL=rendertemplate.d.ts.map
