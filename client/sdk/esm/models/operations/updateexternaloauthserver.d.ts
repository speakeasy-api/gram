import * as z from "zod/v3";
import * as components from "../components/index.js";
export type UpdateExternalOAuthServerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpdateExternalOAuthServerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpdateExternalOAuthServerSecurity = {
  option1?: UpdateExternalOAuthServerSecurityOption1 | undefined;
  option2?: UpdateExternalOAuthServerSecurityOption2 | undefined;
};
export type UpdateExternalOAuthServerRequest = {
  /**
   * The slug of the toolset to update
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
  updateExternalOAuthServerRequestBody: components.UpdateExternalOAuthServerRequestBody;
};
/** @internal */
export type UpdateExternalOAuthServerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateExternalOAuthServerSecurityOption1$outboundSchema: z.ZodType<
  UpdateExternalOAuthServerSecurityOption1$Outbound,
  z.ZodTypeDef,
  UpdateExternalOAuthServerSecurityOption1
>;
export declare function updateExternalOAuthServerSecurityOption1ToJSON(
  updateExternalOAuthServerSecurityOption1: UpdateExternalOAuthServerSecurityOption1,
): string;
/** @internal */
export type UpdateExternalOAuthServerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateExternalOAuthServerSecurityOption2$outboundSchema: z.ZodType<
  UpdateExternalOAuthServerSecurityOption2$Outbound,
  z.ZodTypeDef,
  UpdateExternalOAuthServerSecurityOption2
>;
export declare function updateExternalOAuthServerSecurityOption2ToJSON(
  updateExternalOAuthServerSecurityOption2: UpdateExternalOAuthServerSecurityOption2,
): string;
/** @internal */
export type UpdateExternalOAuthServerSecurity$Outbound = {
  Option1?: UpdateExternalOAuthServerSecurityOption1$Outbound | undefined;
  Option2?: UpdateExternalOAuthServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateExternalOAuthServerSecurity$outboundSchema: z.ZodType<
  UpdateExternalOAuthServerSecurity$Outbound,
  z.ZodTypeDef,
  UpdateExternalOAuthServerSecurity
>;
export declare function updateExternalOAuthServerSecurityToJSON(
  updateExternalOAuthServerSecurity: UpdateExternalOAuthServerSecurity,
): string;
/** @internal */
export type UpdateExternalOAuthServerRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpdateExternalOAuthServerRequestBody: components.UpdateExternalOAuthServerRequestBody$Outbound;
};
/** @internal */
export declare const UpdateExternalOAuthServerRequest$outboundSchema: z.ZodType<
  UpdateExternalOAuthServerRequest$Outbound,
  z.ZodTypeDef,
  UpdateExternalOAuthServerRequest
>;
export declare function updateExternalOAuthServerRequestToJSON(
  updateExternalOAuthServerRequest: UpdateExternalOAuthServerRequest,
): string;
//# sourceMappingURL=updateexternaloauthserver.d.ts.map
