import * as z from "zod/v4-mini";
import {
  UpdatePromptTemplateForm,
  UpdatePromptTemplateForm$Outbound,
} from "../components/updateprompttemplateform.js";
export type UpdateTemplateSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpdateTemplateSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpdateTemplateSecurity = {
  option1?: UpdateTemplateSecurityOption1 | undefined;
  option2?: UpdateTemplateSecurityOption2 | undefined;
};
export type UpdateTemplateRequest = {
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
  updatePromptTemplateForm: UpdatePromptTemplateForm;
};
/** @internal */
export type UpdateTemplateSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateTemplateSecurityOption1$outboundSchema: z.ZodMiniType<
  UpdateTemplateSecurityOption1$Outbound,
  UpdateTemplateSecurityOption1
>;
export declare function updateTemplateSecurityOption1ToJSON(
  updateTemplateSecurityOption1: UpdateTemplateSecurityOption1,
): string;
/** @internal */
export type UpdateTemplateSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateTemplateSecurityOption2$outboundSchema: z.ZodMiniType<
  UpdateTemplateSecurityOption2$Outbound,
  UpdateTemplateSecurityOption2
>;
export declare function updateTemplateSecurityOption2ToJSON(
  updateTemplateSecurityOption2: UpdateTemplateSecurityOption2,
): string;
/** @internal */
export type UpdateTemplateSecurity$Outbound = {
  Option1?: UpdateTemplateSecurityOption1$Outbound | undefined;
  Option2?: UpdateTemplateSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateTemplateSecurity$outboundSchema: z.ZodMiniType<
  UpdateTemplateSecurity$Outbound,
  UpdateTemplateSecurity
>;
export declare function updateTemplateSecurityToJSON(
  updateTemplateSecurity: UpdateTemplateSecurity,
): string;
/** @internal */
export type UpdateTemplateRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdatePromptTemplateForm: UpdatePromptTemplateForm$Outbound;
};
/** @internal */
export declare const UpdateTemplateRequest$outboundSchema: z.ZodMiniType<
  UpdateTemplateRequest$Outbound,
  UpdateTemplateRequest
>;
export declare function updateTemplateRequestToJSON(
  updateTemplateRequest: UpdateTemplateRequest,
): string;
//# sourceMappingURL=updatetemplate.d.ts.map
