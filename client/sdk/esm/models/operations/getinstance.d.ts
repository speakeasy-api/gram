import * as z from "zod/v4-mini";
export type GetInstanceSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetInstanceSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetInstanceSecurityOption3 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetInstanceSecurityOption4 = {
  chatSessionsTokenHeaderGramChatSession: string;
};
export type GetInstanceSecurity = {
  option1?: GetInstanceSecurityOption1 | undefined;
  option2?: GetInstanceSecurityOption2 | undefined;
  option3?: GetInstanceSecurityOption3 | undefined;
  option4?: GetInstanceSecurityOption4 | undefined;
};
export type GetInstanceRequest = {
  /**
   * The slug of the toolset to load
   */
  toolsetSlug: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Chat Sessions token header
   */
  gramChatSession?: string | undefined;
};
/** @internal */
export type GetInstanceSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetInstanceSecurityOption1$outboundSchema: z.ZodMiniType<
  GetInstanceSecurityOption1$Outbound,
  GetInstanceSecurityOption1
>;
export declare function getInstanceSecurityOption1ToJSON(
  getInstanceSecurityOption1: GetInstanceSecurityOption1,
): string;
/** @internal */
export type GetInstanceSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetInstanceSecurityOption2$outboundSchema: z.ZodMiniType<
  GetInstanceSecurityOption2$Outbound,
  GetInstanceSecurityOption2
>;
export declare function getInstanceSecurityOption2ToJSON(
  getInstanceSecurityOption2: GetInstanceSecurityOption2,
): string;
/** @internal */
export type GetInstanceSecurityOption3$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetInstanceSecurityOption3$outboundSchema: z.ZodMiniType<
  GetInstanceSecurityOption3$Outbound,
  GetInstanceSecurityOption3
>;
export declare function getInstanceSecurityOption3ToJSON(
  getInstanceSecurityOption3: GetInstanceSecurityOption3,
): string;
/** @internal */
export type GetInstanceSecurityOption4$Outbound = {
  "chat_sessions_token_header_Gram-Chat-Session": string;
};
/** @internal */
export declare const GetInstanceSecurityOption4$outboundSchema: z.ZodMiniType<
  GetInstanceSecurityOption4$Outbound,
  GetInstanceSecurityOption4
>;
export declare function getInstanceSecurityOption4ToJSON(
  getInstanceSecurityOption4: GetInstanceSecurityOption4,
): string;
/** @internal */
export type GetInstanceSecurity$Outbound = {
  Option1?: GetInstanceSecurityOption1$Outbound | undefined;
  Option2?: GetInstanceSecurityOption2$Outbound | undefined;
  Option3?: GetInstanceSecurityOption3$Outbound | undefined;
  Option4?: GetInstanceSecurityOption4$Outbound | undefined;
};
/** @internal */
export declare const GetInstanceSecurity$outboundSchema: z.ZodMiniType<
  GetInstanceSecurity$Outbound,
  GetInstanceSecurity
>;
export declare function getInstanceSecurityToJSON(
  getInstanceSecurity: GetInstanceSecurity,
): string;
/** @internal */
export type GetInstanceRequest$Outbound = {
  toolset_slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Chat-Session"?: string | undefined;
};
/** @internal */
export declare const GetInstanceRequest$outboundSchema: z.ZodMiniType<
  GetInstanceRequest$Outbound,
  GetInstanceRequest
>;
export declare function getInstanceRequestToJSON(
  getInstanceRequest: GetInstanceRequest,
): string;
//# sourceMappingURL=getinstance.d.ts.map
