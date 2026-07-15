import * as z from "zod/v4-mini";
export type DeleteOrganizationRemoteSessionIssuerSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type DeleteOrganizationRemoteSessionIssuerRequest = {
  /**
   * The remote_session_issuer id.
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
export type DeleteOrganizationRemoteSessionIssuerSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const DeleteOrganizationRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  DeleteOrganizationRemoteSessionIssuerSecurity$Outbound,
  DeleteOrganizationRemoteSessionIssuerSecurity
>;
export declare function deleteOrganizationRemoteSessionIssuerSecurityToJSON(
  deleteOrganizationRemoteSessionIssuerSecurity: DeleteOrganizationRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type DeleteOrganizationRemoteSessionIssuerRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const DeleteOrganizationRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  DeleteOrganizationRemoteSessionIssuerRequest$Outbound,
  DeleteOrganizationRemoteSessionIssuerRequest
>;
export declare function deleteOrganizationRemoteSessionIssuerRequestToJSON(
  deleteOrganizationRemoteSessionIssuerRequest: DeleteOrganizationRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=deleteorganizationremotesessionissuer.d.ts.map
