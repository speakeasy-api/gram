import * as z from "zod/v4-mini";
export type RemoveOAuthServerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type RemoveOAuthServerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type RemoveOAuthServerSecurity = {
  option1?: RemoveOAuthServerSecurityOption1 | undefined;
  option2?: RemoveOAuthServerSecurityOption2 | undefined;
};
export type RemoveOAuthServerRequest = {
  /**
   * The slug of the toolset
   */
  slug: string;
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
export type RemoveOAuthServerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const RemoveOAuthServerSecurityOption1$outboundSchema: z.ZodMiniType<
  RemoveOAuthServerSecurityOption1$Outbound,
  RemoveOAuthServerSecurityOption1
>;
export declare function removeOAuthServerSecurityOption1ToJSON(
  removeOAuthServerSecurityOption1: RemoveOAuthServerSecurityOption1,
): string;
/** @internal */
export type RemoveOAuthServerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RemoveOAuthServerSecurityOption2$outboundSchema: z.ZodMiniType<
  RemoveOAuthServerSecurityOption2$Outbound,
  RemoveOAuthServerSecurityOption2
>;
export declare function removeOAuthServerSecurityOption2ToJSON(
  removeOAuthServerSecurityOption2: RemoveOAuthServerSecurityOption2,
): string;
/** @internal */
export type RemoveOAuthServerSecurity$Outbound = {
  Option1?: RemoveOAuthServerSecurityOption1$Outbound | undefined;
  Option2?: RemoveOAuthServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RemoveOAuthServerSecurity$outboundSchema: z.ZodMiniType<
  RemoveOAuthServerSecurity$Outbound,
  RemoveOAuthServerSecurity
>;
export declare function removeOAuthServerSecurityToJSON(
  removeOAuthServerSecurity: RemoveOAuthServerSecurity,
): string;
/** @internal */
export type RemoveOAuthServerRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const RemoveOAuthServerRequest$outboundSchema: z.ZodMiniType<
  RemoveOAuthServerRequest$Outbound,
  RemoveOAuthServerRequest
>;
export declare function removeOAuthServerRequestToJSON(
  removeOAuthServerRequest: RemoveOAuthServerRequest,
): string;
//# sourceMappingURL=removeoauthserver.d.ts.map
