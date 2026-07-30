import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type KickoffAssistantMessageSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type KickoffAssistantMessageRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  kickoffMessageRequestBody: components.KickoffMessageRequestBody;
};
/** @internal */
export type KickoffAssistantMessageSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const KickoffAssistantMessageSecurity$outboundSchema: z.ZodMiniType<
  KickoffAssistantMessageSecurity$Outbound,
  KickoffAssistantMessageSecurity
>;
export declare function kickoffAssistantMessageSecurityToJSON(
  kickoffAssistantMessageSecurity: KickoffAssistantMessageSecurity,
): string;
/** @internal */
export type KickoffAssistantMessageRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  KickoffMessageRequestBody: components.KickoffMessageRequestBody$Outbound;
};
/** @internal */
export declare const KickoffAssistantMessageRequest$outboundSchema: z.ZodMiniType<
  KickoffAssistantMessageRequest$Outbound,
  KickoffAssistantMessageRequest
>;
export declare function kickoffAssistantMessageRequestToJSON(
  kickoffAssistantMessageRequest: KickoffAssistantMessageRequest,
): string;
//# sourceMappingURL=kickoffassistantmessage.d.ts.map
