import * as z from "zod/v4-mini";
export type DeleteEnvironmentSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteEnvironmentRequest = {
  /**
   * The slug of the environment to delete
   */
  slug: string;
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
export type DeleteEnvironmentSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteEnvironmentSecurity$outboundSchema: z.ZodMiniType<
  DeleteEnvironmentSecurity$Outbound,
  DeleteEnvironmentSecurity
>;
export declare function deleteEnvironmentSecurityToJSON(
  deleteEnvironmentSecurity: DeleteEnvironmentSecurity,
): string;
/** @internal */
export type DeleteEnvironmentRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteEnvironmentRequest$outboundSchema: z.ZodMiniType<
  DeleteEnvironmentRequest$Outbound,
  DeleteEnvironmentRequest
>;
export declare function deleteEnvironmentRequestToJSON(
  deleteEnvironmentRequest: DeleteEnvironmentRequest,
): string;
//# sourceMappingURL=deleteenvironment.d.ts.map
