import * as z from "zod/v4-mini";
export type GetToolsetEnvironmentSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetToolsetEnvironmentRequest = {
  /**
   * The ID of the toolset
   */
  toolsetId: string;
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
export type GetToolsetEnvironmentSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetToolsetEnvironmentSecurity$outboundSchema: z.ZodMiniType<
  GetToolsetEnvironmentSecurity$Outbound,
  GetToolsetEnvironmentSecurity
>;
export declare function getToolsetEnvironmentSecurityToJSON(
  getToolsetEnvironmentSecurity: GetToolsetEnvironmentSecurity,
): string;
/** @internal */
export type GetToolsetEnvironmentRequest$Outbound = {
  toolset_id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetToolsetEnvironmentRequest$outboundSchema: z.ZodMiniType<
  GetToolsetEnvironmentRequest$Outbound,
  GetToolsetEnvironmentRequest
>;
export declare function getToolsetEnvironmentRequestToJSON(
  getToolsetEnvironmentRequest: GetToolsetEnvironmentRequest,
): string;
//# sourceMappingURL=gettoolsetenvironment.d.ts.map
