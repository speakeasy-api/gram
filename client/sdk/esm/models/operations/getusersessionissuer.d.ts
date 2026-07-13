import * as z from "zod/v4-mini";
export type GetUserSessionIssuerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetUserSessionIssuerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetUserSessionIssuerSecurity = {
  option1?: GetUserSessionIssuerSecurityOption1 | undefined;
  option2?: GetUserSessionIssuerSecurityOption2 | undefined;
};
export type GetUserSessionIssuerRequest = {
  /**
   * The user_session_issuer id.
   */
  id?: string | undefined;
  /**
   * The user_session_issuer slug.
   */
  slug?: string | undefined;
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
export type GetUserSessionIssuerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetUserSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<
  GetUserSessionIssuerSecurityOption1$Outbound,
  GetUserSessionIssuerSecurityOption1
>;
export declare function getUserSessionIssuerSecurityOption1ToJSON(
  getUserSessionIssuerSecurityOption1: GetUserSessionIssuerSecurityOption1,
): string;
/** @internal */
export type GetUserSessionIssuerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetUserSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<
  GetUserSessionIssuerSecurityOption2$Outbound,
  GetUserSessionIssuerSecurityOption2
>;
export declare function getUserSessionIssuerSecurityOption2ToJSON(
  getUserSessionIssuerSecurityOption2: GetUserSessionIssuerSecurityOption2,
): string;
/** @internal */
export type GetUserSessionIssuerSecurity$Outbound = {
  Option1?: GetUserSessionIssuerSecurityOption1$Outbound | undefined;
  Option2?: GetUserSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetUserSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  GetUserSessionIssuerSecurity$Outbound,
  GetUserSessionIssuerSecurity
>;
export declare function getUserSessionIssuerSecurityToJSON(
  getUserSessionIssuerSecurity: GetUserSessionIssuerSecurity,
): string;
/** @internal */
export type GetUserSessionIssuerRequest$Outbound = {
  id?: string | undefined;
  slug?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetUserSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  GetUserSessionIssuerRequest$Outbound,
  GetUserSessionIssuerRequest
>;
export declare function getUserSessionIssuerRequestToJSON(
  getUserSessionIssuerRequest: GetUserSessionIssuerRequest,
): string;
//# sourceMappingURL=getusersessionissuer.d.ts.map
