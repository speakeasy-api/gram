import * as z from "zod/v3";
export type DeleteSlackConnectionSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteSlackConnectionRequest = {
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
export type DeleteSlackConnectionSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteSlackConnectionSecurity$outboundSchema: z.ZodType<
  DeleteSlackConnectionSecurity$Outbound,
  z.ZodTypeDef,
  DeleteSlackConnectionSecurity
>;
export declare function deleteSlackConnectionSecurityToJSON(
  deleteSlackConnectionSecurity: DeleteSlackConnectionSecurity,
): string;
/** @internal */
export type DeleteSlackConnectionRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteSlackConnectionRequest$outboundSchema: z.ZodType<
  DeleteSlackConnectionRequest$Outbound,
  z.ZodTypeDef,
  DeleteSlackConnectionRequest
>;
export declare function deleteSlackConnectionRequestToJSON(
  deleteSlackConnectionRequest: DeleteSlackConnectionRequest,
): string;
//# sourceMappingURL=deleteslackconnection.d.ts.map
