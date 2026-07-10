import * as z from "zod/v4-mini";
import {
  UpdateAssistantForm,
  UpdateAssistantForm$Outbound,
} from "../components/updateassistantform.js";
export type UpdateAssistantSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type UpdateAssistantRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  updateAssistantForm: UpdateAssistantForm;
};
/** @internal */
export type UpdateAssistantSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const UpdateAssistantSecurity$outboundSchema: z.ZodMiniType<
  UpdateAssistantSecurity$Outbound,
  UpdateAssistantSecurity
>;
export declare function updateAssistantSecurityToJSON(
  updateAssistantSecurity: UpdateAssistantSecurity,
): string;
/** @internal */
export type UpdateAssistantRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateAssistantForm: UpdateAssistantForm$Outbound;
};
/** @internal */
export declare const UpdateAssistantRequest$outboundSchema: z.ZodMiniType<
  UpdateAssistantRequest$Outbound,
  UpdateAssistantRequest
>;
export declare function updateAssistantRequestToJSON(
  updateAssistantRequest: UpdateAssistantRequest,
): string;
//# sourceMappingURL=updateassistant.d.ts.map
