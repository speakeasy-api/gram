import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type ApproveShadowMCPSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ApproveShadowMCPSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ApproveShadowMCPSecurity = {
  option1?: ApproveShadowMCPSecurityOption1 | undefined;
  option2?: ApproveShadowMCPSecurityOption2 | undefined;
};
export type ApproveShadowMCPRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  approveShadowMCPRequestBody: components.ApproveShadowMCPRequestBody;
};
/** @internal */
export type ApproveShadowMCPSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ApproveShadowMCPSecurityOption1$outboundSchema: z.ZodMiniType<
  ApproveShadowMCPSecurityOption1$Outbound,
  ApproveShadowMCPSecurityOption1
>;
export declare function approveShadowMCPSecurityOption1ToJSON(
  approveShadowMCPSecurityOption1: ApproveShadowMCPSecurityOption1,
): string;
/** @internal */
export type ApproveShadowMCPSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ApproveShadowMCPSecurityOption2$outboundSchema: z.ZodMiniType<
  ApproveShadowMCPSecurityOption2$Outbound,
  ApproveShadowMCPSecurityOption2
>;
export declare function approveShadowMCPSecurityOption2ToJSON(
  approveShadowMCPSecurityOption2: ApproveShadowMCPSecurityOption2,
): string;
/** @internal */
export type ApproveShadowMCPSecurity$Outbound = {
  Option1?: ApproveShadowMCPSecurityOption1$Outbound | undefined;
  Option2?: ApproveShadowMCPSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ApproveShadowMCPSecurity$outboundSchema: z.ZodMiniType<
  ApproveShadowMCPSecurity$Outbound,
  ApproveShadowMCPSecurity
>;
export declare function approveShadowMCPSecurityToJSON(
  approveShadowMCPSecurity: ApproveShadowMCPSecurity,
): string;
/** @internal */
export type ApproveShadowMCPRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  ApproveShadowMCPRequestBody: components.ApproveShadowMCPRequestBody$Outbound;
};
/** @internal */
export declare const ApproveShadowMCPRequest$outboundSchema: z.ZodMiniType<
  ApproveShadowMCPRequest$Outbound,
  ApproveShadowMCPRequest
>;
export declare function approveShadowMCPRequestToJSON(
  approveShadowMCPRequest: ApproveShadowMCPRequest,
): string;
//# sourceMappingURL=approveshadowmcp.d.ts.map
