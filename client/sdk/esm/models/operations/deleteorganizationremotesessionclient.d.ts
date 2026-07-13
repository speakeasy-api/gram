import * as z from "zod/v4-mini";
export type DeleteOrganizationRemoteSessionClientSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type DeleteOrganizationRemoteSessionClientRequest = {
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
export type DeleteOrganizationRemoteSessionClientSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const DeleteOrganizationRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<
  DeleteOrganizationRemoteSessionClientSecurity$Outbound,
  DeleteOrganizationRemoteSessionClientSecurity
>;
export declare function deleteOrganizationRemoteSessionClientSecurityToJSON(
  deleteOrganizationRemoteSessionClientSecurity: DeleteOrganizationRemoteSessionClientSecurity,
): string;
/** @internal */
export type DeleteOrganizationRemoteSessionClientRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const DeleteOrganizationRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<
  DeleteOrganizationRemoteSessionClientRequest$Outbound,
  DeleteOrganizationRemoteSessionClientRequest
>;
export declare function deleteOrganizationRemoteSessionClientRequestToJSON(
  deleteOrganizationRemoteSessionClientRequest: DeleteOrganizationRemoteSessionClientRequest,
): string;
//# sourceMappingURL=deleteorganizationremotesessionclient.d.ts.map
