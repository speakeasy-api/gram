import * as z from "zod/v4-mini";
export type RevokeOrganizationRemoteSessionSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type RevokeOrganizationRemoteSessionRequest = {
  /**
   * The remote_session id.
   */
  id: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
};
/** @internal */
export type RevokeOrganizationRemoteSessionSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const RevokeOrganizationRemoteSessionSecurity$outboundSchema: z.ZodMiniType<
  RevokeOrganizationRemoteSessionSecurity$Outbound,
  RevokeOrganizationRemoteSessionSecurity
>;
export declare function revokeOrganizationRemoteSessionSecurityToJSON(
  revokeOrganizationRemoteSessionSecurity: RevokeOrganizationRemoteSessionSecurity,
): string;
/** @internal */
export type RevokeOrganizationRemoteSessionRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const RevokeOrganizationRemoteSessionRequest$outboundSchema: z.ZodMiniType<
  RevokeOrganizationRemoteSessionRequest$Outbound,
  RevokeOrganizationRemoteSessionRequest
>;
export declare function revokeOrganizationRemoteSessionRequestToJSON(
  revokeOrganizationRemoteSessionRequest: RevokeOrganizationRemoteSessionRequest,
): string;
//# sourceMappingURL=revokeorganizationremotesession.d.ts.map
