import * as z from "zod/v4-mini";
export type GetAssistantSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetAssistantRequest = {
  /**
   * The assistant ID.
   */
  id: string;
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
export type GetAssistantSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetAssistantSecurity$outboundSchema: z.ZodMiniType<
  GetAssistantSecurity$Outbound,
  GetAssistantSecurity
>;
export declare function getAssistantSecurityToJSON(
  getAssistantSecurity: GetAssistantSecurity,
): string;
/** @internal */
export type GetAssistantRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetAssistantRequest$outboundSchema: z.ZodMiniType<
  GetAssistantRequest$Outbound,
  GetAssistantRequest
>;
export declare function getAssistantRequestToJSON(
  getAssistantRequest: GetAssistantRequest,
): string;
//# sourceMappingURL=getassistant.d.ts.map
