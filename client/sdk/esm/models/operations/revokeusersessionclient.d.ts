import * as z from "zod/v4-mini";
export type RevokeUserSessionClientSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type RevokeUserSessionClientSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type RevokeUserSessionClientSecurity = {
  option1?: RevokeUserSessionClientSecurityOption1 | undefined;
  option2?: RevokeUserSessionClientSecurityOption2 | undefined;
};
export type RevokeUserSessionClientRequest = {
  /**
   * The user_session_client id.
   */
  id: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type RevokeUserSessionClientSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const RevokeUserSessionClientSecurityOption1$outboundSchema: z.ZodMiniType<
  RevokeUserSessionClientSecurityOption1$Outbound,
  RevokeUserSessionClientSecurityOption1
>;
export declare function revokeUserSessionClientSecurityOption1ToJSON(
  revokeUserSessionClientSecurityOption1: RevokeUserSessionClientSecurityOption1,
): string;
/** @internal */
export type RevokeUserSessionClientSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RevokeUserSessionClientSecurityOption2$outboundSchema: z.ZodMiniType<
  RevokeUserSessionClientSecurityOption2$Outbound,
  RevokeUserSessionClientSecurityOption2
>;
export declare function revokeUserSessionClientSecurityOption2ToJSON(
  revokeUserSessionClientSecurityOption2: RevokeUserSessionClientSecurityOption2,
): string;
/** @internal */
export type RevokeUserSessionClientSecurity$Outbound = {
  Option1?: RevokeUserSessionClientSecurityOption1$Outbound | undefined;
  Option2?: RevokeUserSessionClientSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RevokeUserSessionClientSecurity$outboundSchema: z.ZodMiniType<
  RevokeUserSessionClientSecurity$Outbound,
  RevokeUserSessionClientSecurity
>;
export declare function revokeUserSessionClientSecurityToJSON(
  revokeUserSessionClientSecurity: RevokeUserSessionClientSecurity,
): string;
/** @internal */
export type RevokeUserSessionClientRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const RevokeUserSessionClientRequest$outboundSchema: z.ZodMiniType<
  RevokeUserSessionClientRequest$Outbound,
  RevokeUserSessionClientRequest
>;
export declare function revokeUserSessionClientRequestToJSON(
  revokeUserSessionClientRequest: RevokeUserSessionClientRequest,
): string;
//# sourceMappingURL=revokeusersessionclient.d.ts.map
