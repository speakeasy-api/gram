import * as z from "zod/v4-mini";
export type DeleteProjectSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteProjectRequest = {
  /**
   * The id of the project to delete
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
};
/** @internal */
export type DeleteProjectSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteProjectSecurity$outboundSchema: z.ZodMiniType<
  DeleteProjectSecurity$Outbound,
  DeleteProjectSecurity
>;
export declare function deleteProjectSecurityToJSON(
  deleteProjectSecurity: DeleteProjectSecurity,
): string;
/** @internal */
export type DeleteProjectRequest$Outbound = {
  id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteProjectRequest$outboundSchema: z.ZodMiniType<
  DeleteProjectRequest$Outbound,
  DeleteProjectRequest
>;
export declare function deleteProjectRequestToJSON(
  deleteProjectRequest: DeleteProjectRequest,
): string;
//# sourceMappingURL=deleteproject.d.ts.map
