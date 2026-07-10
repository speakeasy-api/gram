import * as z from "zod/v4-mini";
export type GetLatestDeploymentSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetLatestDeploymentSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetLatestDeploymentSecurity = {
  option1?: GetLatestDeploymentSecurityOption1 | undefined;
  option2?: GetLatestDeploymentSecurityOption2 | undefined;
};
export type GetLatestDeploymentRequest = {
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
};
/** @internal */
export type GetLatestDeploymentSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetLatestDeploymentSecurityOption1$outboundSchema: z.ZodMiniType<
  GetLatestDeploymentSecurityOption1$Outbound,
  GetLatestDeploymentSecurityOption1
>;
export declare function getLatestDeploymentSecurityOption1ToJSON(
  getLatestDeploymentSecurityOption1: GetLatestDeploymentSecurityOption1,
): string;
/** @internal */
export type GetLatestDeploymentSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetLatestDeploymentSecurityOption2$outboundSchema: z.ZodMiniType<
  GetLatestDeploymentSecurityOption2$Outbound,
  GetLatestDeploymentSecurityOption2
>;
export declare function getLatestDeploymentSecurityOption2ToJSON(
  getLatestDeploymentSecurityOption2: GetLatestDeploymentSecurityOption2,
): string;
/** @internal */
export type GetLatestDeploymentSecurity$Outbound = {
  Option1?: GetLatestDeploymentSecurityOption1$Outbound | undefined;
  Option2?: GetLatestDeploymentSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetLatestDeploymentSecurity$outboundSchema: z.ZodMiniType<
  GetLatestDeploymentSecurity$Outbound,
  GetLatestDeploymentSecurity
>;
export declare function getLatestDeploymentSecurityToJSON(
  getLatestDeploymentSecurity: GetLatestDeploymentSecurity,
): string;
/** @internal */
export type GetLatestDeploymentRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetLatestDeploymentRequest$outboundSchema: z.ZodMiniType<
  GetLatestDeploymentRequest$Outbound,
  GetLatestDeploymentRequest
>;
export declare function getLatestDeploymentRequestToJSON(
  getLatestDeploymentRequest: GetLatestDeploymentRequest,
): string;
//# sourceMappingURL=getlatestdeployment.d.ts.map
