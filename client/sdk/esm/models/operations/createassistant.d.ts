import * as z from "zod/v4-mini";
import {
  CreateAssistantForm,
  CreateAssistantForm$Outbound,
} from "../components/createassistantform.js";
export type CreateAssistantSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type CreateAssistantRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  createAssistantForm: CreateAssistantForm;
};
/** @internal */
export type CreateAssistantSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateAssistantSecurity$outboundSchema: z.ZodMiniType<
  CreateAssistantSecurity$Outbound,
  CreateAssistantSecurity
>;
export declare function createAssistantSecurityToJSON(
  createAssistantSecurity: CreateAssistantSecurity,
): string;
/** @internal */
export type CreateAssistantRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateAssistantForm: CreateAssistantForm$Outbound;
};
/** @internal */
export declare const CreateAssistantRequest$outboundSchema: z.ZodMiniType<
  CreateAssistantRequest$Outbound,
  CreateAssistantRequest
>;
export declare function createAssistantRequestToJSON(
  createAssistantRequest: CreateAssistantRequest,
): string;
//# sourceMappingURL=createassistant.d.ts.map
