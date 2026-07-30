import * as z from "zod/v3";
export type GetSlackConnectionSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetSlackConnectionRequest = {
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
export type GetSlackConnectionSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetSlackConnectionSecurity$outboundSchema: z.ZodType<
  GetSlackConnectionSecurity$Outbound,
  z.ZodTypeDef,
  GetSlackConnectionSecurity
>;
export declare function getSlackConnectionSecurityToJSON(
  getSlackConnectionSecurity: GetSlackConnectionSecurity,
): string;
/** @internal */
export type GetSlackConnectionRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetSlackConnectionRequest$outboundSchema: z.ZodType<
  GetSlackConnectionRequest$Outbound,
  z.ZodTypeDef,
  GetSlackConnectionRequest
>;
export declare function getSlackConnectionRequestToJSON(
  getSlackConnectionRequest: GetSlackConnectionRequest,
): string;
//# sourceMappingURL=getslackconnection.d.ts.map
