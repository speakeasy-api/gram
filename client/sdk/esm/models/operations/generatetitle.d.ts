import * as z from "zod/v4-mini";
import {
  GenerateTitleRequestBody,
  GenerateTitleRequestBody$Outbound,
} from "../components/generatetitlerequestbody.js";
export type GenerateTitleSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GenerateTitleSecurityOption2 = {
  chatSessionsTokenHeaderGramChatSession: string;
};
export type GenerateTitleSecurity = {
  option1?: GenerateTitleSecurityOption1 | undefined;
  option2?: GenerateTitleSecurityOption2 | undefined;
};
export type GenerateTitleRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  /**
   * Chat Sessions token header
   */
  gramChatSession?: string | undefined;
  generateTitleRequestBody: GenerateTitleRequestBody;
};
/** @internal */
export type GenerateTitleSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GenerateTitleSecurityOption1$outboundSchema: z.ZodMiniType<
  GenerateTitleSecurityOption1$Outbound,
  GenerateTitleSecurityOption1
>;
export declare function generateTitleSecurityOption1ToJSON(
  generateTitleSecurityOption1: GenerateTitleSecurityOption1,
): string;
/** @internal */
export type GenerateTitleSecurityOption2$Outbound = {
  "chat_sessions_token_header_Gram-Chat-Session": string;
};
/** @internal */
export declare const GenerateTitleSecurityOption2$outboundSchema: z.ZodMiniType<
  GenerateTitleSecurityOption2$Outbound,
  GenerateTitleSecurityOption2
>;
export declare function generateTitleSecurityOption2ToJSON(
  generateTitleSecurityOption2: GenerateTitleSecurityOption2,
): string;
/** @internal */
export type GenerateTitleSecurity$Outbound = {
  Option1?: GenerateTitleSecurityOption1$Outbound | undefined;
  Option2?: GenerateTitleSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GenerateTitleSecurity$outboundSchema: z.ZodMiniType<
  GenerateTitleSecurity$Outbound,
  GenerateTitleSecurity
>;
export declare function generateTitleSecurityToJSON(
  generateTitleSecurity: GenerateTitleSecurity,
): string;
/** @internal */
export type GenerateTitleRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  "Gram-Chat-Session"?: string | undefined;
  GenerateTitleRequestBody: GenerateTitleRequestBody$Outbound;
};
/** @internal */
export declare const GenerateTitleRequest$outboundSchema: z.ZodMiniType<
  GenerateTitleRequest$Outbound,
  GenerateTitleRequest
>;
export declare function generateTitleRequestToJSON(
  generateTitleRequest: GenerateTitleRequest,
): string;
//# sourceMappingURL=generatetitle.d.ts.map
