import * as z from "zod/v4-mini";
import {
  CloneClientFromOAuthProxyProviderForm,
  CloneClientFromOAuthProxyProviderForm$Outbound,
} from "../components/cloneclientfromoauthproxyproviderform.js";
export type CloneClientFromOAuthProxyProviderSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CloneClientFromOAuthProxyProviderSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CloneClientFromOAuthProxyProviderSecurity = {
  option1?: CloneClientFromOAuthProxyProviderSecurityOption1 | undefined;
  option2?: CloneClientFromOAuthProxyProviderSecurityOption2 | undefined;
};
export type CloneClientFromOAuthProxyProviderRequest = {
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
  cloneClientFromOAuthProxyProviderForm: CloneClientFromOAuthProxyProviderForm;
};
/** @internal */
export type CloneClientFromOAuthProxyProviderSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CloneClientFromOAuthProxyProviderSecurityOption1$outboundSchema: z.ZodMiniType<
  CloneClientFromOAuthProxyProviderSecurityOption1$Outbound,
  CloneClientFromOAuthProxyProviderSecurityOption1
>;
export declare function cloneClientFromOAuthProxyProviderSecurityOption1ToJSON(
  cloneClientFromOAuthProxyProviderSecurityOption1: CloneClientFromOAuthProxyProviderSecurityOption1,
): string;
/** @internal */
export type CloneClientFromOAuthProxyProviderSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CloneClientFromOAuthProxyProviderSecurityOption2$outboundSchema: z.ZodMiniType<
  CloneClientFromOAuthProxyProviderSecurityOption2$Outbound,
  CloneClientFromOAuthProxyProviderSecurityOption2
>;
export declare function cloneClientFromOAuthProxyProviderSecurityOption2ToJSON(
  cloneClientFromOAuthProxyProviderSecurityOption2: CloneClientFromOAuthProxyProviderSecurityOption2,
): string;
/** @internal */
export type CloneClientFromOAuthProxyProviderSecurity$Outbound = {
  Option1?:
    | CloneClientFromOAuthProxyProviderSecurityOption1$Outbound
    | undefined;
  Option2?:
    | CloneClientFromOAuthProxyProviderSecurityOption2$Outbound
    | undefined;
};
/** @internal */
export declare const CloneClientFromOAuthProxyProviderSecurity$outboundSchema: z.ZodMiniType<
  CloneClientFromOAuthProxyProviderSecurity$Outbound,
  CloneClientFromOAuthProxyProviderSecurity
>;
export declare function cloneClientFromOAuthProxyProviderSecurityToJSON(
  cloneClientFromOAuthProxyProviderSecurity: CloneClientFromOAuthProxyProviderSecurity,
): string;
/** @internal */
export type CloneClientFromOAuthProxyProviderRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CloneClientFromOAuthProxyProviderForm: CloneClientFromOAuthProxyProviderForm$Outbound;
};
/** @internal */
export declare const CloneClientFromOAuthProxyProviderRequest$outboundSchema: z.ZodMiniType<
  CloneClientFromOAuthProxyProviderRequest$Outbound,
  CloneClientFromOAuthProxyProviderRequest
>;
export declare function cloneClientFromOAuthProxyProviderRequestToJSON(
  cloneClientFromOAuthProxyProviderRequest: CloneClientFromOAuthProxyProviderRequest,
): string;
//# sourceMappingURL=cloneclientfromoauthproxyprovider.d.ts.map
