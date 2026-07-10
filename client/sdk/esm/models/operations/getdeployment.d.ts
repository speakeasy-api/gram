import * as z from "zod/v4-mini";
export type GetDeploymentSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetDeploymentSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetDeploymentSecurity = {
  option1?: GetDeploymentSecurityOption1 | undefined;
  option2?: GetDeploymentSecurityOption2 | undefined;
};
export type GetDeploymentRequest = {
  /**
   * The ID of the deployment
   */
  id: string;
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
export type GetDeploymentSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetDeploymentSecurityOption1$outboundSchema: z.ZodMiniType<
  GetDeploymentSecurityOption1$Outbound,
  GetDeploymentSecurityOption1
>;
export declare function getDeploymentSecurityOption1ToJSON(
  getDeploymentSecurityOption1: GetDeploymentSecurityOption1,
): string;
/** @internal */
export type GetDeploymentSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetDeploymentSecurityOption2$outboundSchema: z.ZodMiniType<
  GetDeploymentSecurityOption2$Outbound,
  GetDeploymentSecurityOption2
>;
export declare function getDeploymentSecurityOption2ToJSON(
  getDeploymentSecurityOption2: GetDeploymentSecurityOption2,
): string;
/** @internal */
export type GetDeploymentSecurity$Outbound = {
  Option1?: GetDeploymentSecurityOption1$Outbound | undefined;
  Option2?: GetDeploymentSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetDeploymentSecurity$outboundSchema: z.ZodMiniType<
  GetDeploymentSecurity$Outbound,
  GetDeploymentSecurity
>;
export declare function getDeploymentSecurityToJSON(
  getDeploymentSecurity: GetDeploymentSecurity,
): string;
/** @internal */
export type GetDeploymentRequest$Outbound = {
  id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetDeploymentRequest$outboundSchema: z.ZodMiniType<
  GetDeploymentRequest$Outbound,
  GetDeploymentRequest
>;
export declare function getDeploymentRequestToJSON(
  getDeploymentRequest: GetDeploymentRequest,
): string;
//# sourceMappingURL=getdeployment.d.ts.map
