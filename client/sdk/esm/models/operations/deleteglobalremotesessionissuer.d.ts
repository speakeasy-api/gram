import * as z from "zod/v4-mini";
export type DeleteGlobalRemoteSessionIssuerSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteGlobalRemoteSessionIssuerRequest = {
  /**
   * The remote_session_issuer id.
   */
  id: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type DeleteGlobalRemoteSessionIssuerSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteGlobalRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  DeleteGlobalRemoteSessionIssuerSecurity$Outbound,
  DeleteGlobalRemoteSessionIssuerSecurity
>;
export declare function deleteGlobalRemoteSessionIssuerSecurityToJSON(
  deleteGlobalRemoteSessionIssuerSecurity: DeleteGlobalRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type DeleteGlobalRemoteSessionIssuerRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteGlobalRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  DeleteGlobalRemoteSessionIssuerRequest$Outbound,
  DeleteGlobalRemoteSessionIssuerRequest
>;
export declare function deleteGlobalRemoteSessionIssuerRequestToJSON(
  deleteGlobalRemoteSessionIssuerRequest: DeleteGlobalRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=deleteglobalremotesessionissuer.d.ts.map
