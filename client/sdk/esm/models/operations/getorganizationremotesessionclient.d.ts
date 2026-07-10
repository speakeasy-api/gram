import * as z from "zod/v4-mini";
export type GetOrganizationRemoteSessionClientSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type GetOrganizationRemoteSessionClientRequest = {
  /**
   * The remote_session_client id.
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
export type GetOrganizationRemoteSessionClientSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<
  GetOrganizationRemoteSessionClientSecurity$Outbound,
  GetOrganizationRemoteSessionClientSecurity
>;
export declare function getOrganizationRemoteSessionClientSecurityToJSON(
  getOrganizationRemoteSessionClientSecurity: GetOrganizationRemoteSessionClientSecurity,
): string;
/** @internal */
export type GetOrganizationRemoteSessionClientRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<
  GetOrganizationRemoteSessionClientRequest$Outbound,
  GetOrganizationRemoteSessionClientRequest
>;
export declare function getOrganizationRemoteSessionClientRequestToJSON(
  getOrganizationRemoteSessionClientRequest: GetOrganizationRemoteSessionClientRequest,
): string;
//# sourceMappingURL=getorganizationremotesessionclient.d.ts.map
