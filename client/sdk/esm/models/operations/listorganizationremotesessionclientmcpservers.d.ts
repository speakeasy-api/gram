import * as z from "zod/v4-mini";
export type ListOrganizationRemoteSessionClientMcpServersSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type ListOrganizationRemoteSessionClientMcpServersRequest = {
  /**
   * The remote_session_client id.
   */
  clientId: string;
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
export type ListOrganizationRemoteSessionClientMcpServersSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionClientMcpServersSecurity$outboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionClientMcpServersSecurity$Outbound,
  ListOrganizationRemoteSessionClientMcpServersSecurity
>;
export declare function listOrganizationRemoteSessionClientMcpServersSecurityToJSON(
  listOrganizationRemoteSessionClientMcpServersSecurity: ListOrganizationRemoteSessionClientMcpServersSecurity,
): string;
/** @internal */
export type ListOrganizationRemoteSessionClientMcpServersRequest$Outbound = {
  client_id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationRemoteSessionClientMcpServersRequest$outboundSchema: z.ZodMiniType<
  ListOrganizationRemoteSessionClientMcpServersRequest$Outbound,
  ListOrganizationRemoteSessionClientMcpServersRequest
>;
export declare function listOrganizationRemoteSessionClientMcpServersRequestToJSON(
  listOrganizationRemoteSessionClientMcpServersRequest: ListOrganizationRemoteSessionClientMcpServersRequest,
): string;
//# sourceMappingURL=listorganizationremotesessionclientmcpservers.d.ts.map
