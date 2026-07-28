import * as z from "zod/v4-mini";
import {
  AddOAuthProxyServerRequestBody,
  AddOAuthProxyServerRequestBody$Outbound,
} from "../components/addoauthproxyserverrequestbody.js";
export type AddOAuthProxyServerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type AddOAuthProxyServerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type AddOAuthProxyServerSecurity = {
  option1?: AddOAuthProxyServerSecurityOption1 | undefined;
  option2?: AddOAuthProxyServerSecurityOption2 | undefined;
};
export type AddOAuthProxyServerRequest = {
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
  addOAuthProxyServerRequestBody: AddOAuthProxyServerRequestBody;
};
/** @internal */
export type AddOAuthProxyServerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const AddOAuthProxyServerSecurityOption1$outboundSchema: z.ZodMiniType<
  AddOAuthProxyServerSecurityOption1$Outbound,
  AddOAuthProxyServerSecurityOption1
>;
export declare function addOAuthProxyServerSecurityOption1ToJSON(
  addOAuthProxyServerSecurityOption1: AddOAuthProxyServerSecurityOption1,
): string;
/** @internal */
export type AddOAuthProxyServerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const AddOAuthProxyServerSecurityOption2$outboundSchema: z.ZodMiniType<
  AddOAuthProxyServerSecurityOption2$Outbound,
  AddOAuthProxyServerSecurityOption2
>;
export declare function addOAuthProxyServerSecurityOption2ToJSON(
  addOAuthProxyServerSecurityOption2: AddOAuthProxyServerSecurityOption2,
): string;
/** @internal */
export type AddOAuthProxyServerSecurity$Outbound = {
  Option1?: AddOAuthProxyServerSecurityOption1$Outbound | undefined;
  Option2?: AddOAuthProxyServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const AddOAuthProxyServerSecurity$outboundSchema: z.ZodMiniType<
  AddOAuthProxyServerSecurity$Outbound,
  AddOAuthProxyServerSecurity
>;
export declare function addOAuthProxyServerSecurityToJSON(
  addOAuthProxyServerSecurity: AddOAuthProxyServerSecurity,
): string;
/** @internal */
export type AddOAuthProxyServerRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  AddOAuthProxyServerRequestBody: AddOAuthProxyServerRequestBody$Outbound;
};
/** @internal */
export declare const AddOAuthProxyServerRequest$outboundSchema: z.ZodMiniType<
  AddOAuthProxyServerRequest$Outbound,
  AddOAuthProxyServerRequest
>;
export declare function addOAuthProxyServerRequestToJSON(
  addOAuthProxyServerRequest: AddOAuthProxyServerRequest,
): string;
//# sourceMappingURL=addoauthproxyserver.d.ts.map
