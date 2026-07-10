import * as z from "zod/v4-mini";
export type DeleteToolsetEnvironmentLinkSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteToolsetEnvironmentLinkRequest = {
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
export type DeleteToolsetEnvironmentLinkSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteToolsetEnvironmentLinkSecurity$outboundSchema: z.ZodMiniType<
  DeleteToolsetEnvironmentLinkSecurity$Outbound,
  DeleteToolsetEnvironmentLinkSecurity
>;
export declare function deleteToolsetEnvironmentLinkSecurityToJSON(
  deleteToolsetEnvironmentLinkSecurity: DeleteToolsetEnvironmentLinkSecurity,
): string;
/** @internal */
export type DeleteToolsetEnvironmentLinkRequest$Outbound = {
  toolset_id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteToolsetEnvironmentLinkRequest$outboundSchema: z.ZodMiniType<
  DeleteToolsetEnvironmentLinkRequest$Outbound,
  DeleteToolsetEnvironmentLinkRequest
>;
export declare function deleteToolsetEnvironmentLinkRequestToJSON(
  deleteToolsetEnvironmentLinkRequest: DeleteToolsetEnvironmentLinkRequest,
): string;
//# sourceMappingURL=deletetoolsetenvironmentlink.d.ts.map
