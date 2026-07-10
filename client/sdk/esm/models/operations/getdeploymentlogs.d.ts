import * as z from "zod/v4-mini";
export type GetDeploymentLogsSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetDeploymentLogsSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetDeploymentLogsSecurity = {
  option1?: GetDeploymentLogsSecurityOption1 | undefined;
  option2?: GetDeploymentLogsSecurityOption2 | undefined;
};
export type GetDeploymentLogsRequest = {
  /**
   * The ID of the deployment
   */
  deploymentId: string;
  /**
   * The cursor to fetch results from
   */
  cursor?: string | undefined;
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
export type GetDeploymentLogsSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetDeploymentLogsSecurityOption1$outboundSchema: z.ZodMiniType<
  GetDeploymentLogsSecurityOption1$Outbound,
  GetDeploymentLogsSecurityOption1
>;
export declare function getDeploymentLogsSecurityOption1ToJSON(
  getDeploymentLogsSecurityOption1: GetDeploymentLogsSecurityOption1,
): string;
/** @internal */
export type GetDeploymentLogsSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetDeploymentLogsSecurityOption2$outboundSchema: z.ZodMiniType<
  GetDeploymentLogsSecurityOption2$Outbound,
  GetDeploymentLogsSecurityOption2
>;
export declare function getDeploymentLogsSecurityOption2ToJSON(
  getDeploymentLogsSecurityOption2: GetDeploymentLogsSecurityOption2,
): string;
/** @internal */
export type GetDeploymentLogsSecurity$Outbound = {
  Option1?: GetDeploymentLogsSecurityOption1$Outbound | undefined;
  Option2?: GetDeploymentLogsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetDeploymentLogsSecurity$outboundSchema: z.ZodMiniType<
  GetDeploymentLogsSecurity$Outbound,
  GetDeploymentLogsSecurity
>;
export declare function getDeploymentLogsSecurityToJSON(
  getDeploymentLogsSecurity: GetDeploymentLogsSecurity,
): string;
/** @internal */
export type GetDeploymentLogsRequest$Outbound = {
  deployment_id: string;
  cursor?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetDeploymentLogsRequest$outboundSchema: z.ZodMiniType<
  GetDeploymentLogsRequest$Outbound,
  GetDeploymentLogsRequest
>;
export declare function getDeploymentLogsRequestToJSON(
  getDeploymentLogsRequest: GetDeploymentLogsRequest,
): string;
//# sourceMappingURL=getdeploymentlogs.d.ts.map
