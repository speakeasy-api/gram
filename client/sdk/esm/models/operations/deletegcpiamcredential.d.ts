import * as z from "zod/v4-mini";
export type DeleteGcpIamCredentialSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteGcpIamCredentialRequest = {
  /**
   * The ID of the credential to delete.
   */
  id: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type DeleteGcpIamCredentialSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteGcpIamCredentialSecurity$outboundSchema: z.ZodMiniType<
  DeleteGcpIamCredentialSecurity$Outbound,
  DeleteGcpIamCredentialSecurity
>;
export declare function deleteGcpIamCredentialSecurityToJSON(
  deleteGcpIamCredentialSecurity: DeleteGcpIamCredentialSecurity,
): string;
/** @internal */
export type DeleteGcpIamCredentialRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteGcpIamCredentialRequest$outboundSchema: z.ZodMiniType<
  DeleteGcpIamCredentialRequest$Outbound,
  DeleteGcpIamCredentialRequest
>;
export declare function deleteGcpIamCredentialRequestToJSON(
  deleteGcpIamCredentialRequest: DeleteGcpIamCredentialRequest,
): string;
//# sourceMappingURL=deletegcpiamcredential.d.ts.map
