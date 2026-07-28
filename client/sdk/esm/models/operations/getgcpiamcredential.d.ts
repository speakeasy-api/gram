import * as z from "zod/v4-mini";
export type GetGcpIamCredentialSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type GetGcpIamCredentialRequest = {
  /**
   * The ID of the credential to get.
   */
  id: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type GetGcpIamCredentialSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetGcpIamCredentialSecurity$outboundSchema: z.ZodMiniType<
  GetGcpIamCredentialSecurity$Outbound,
  GetGcpIamCredentialSecurity
>;
export declare function getGcpIamCredentialSecurityToJSON(
  getGcpIamCredentialSecurity: GetGcpIamCredentialSecurity,
): string;
/** @internal */
export type GetGcpIamCredentialRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetGcpIamCredentialRequest$outboundSchema: z.ZodMiniType<
  GetGcpIamCredentialRequest$Outbound,
  GetGcpIamCredentialRequest
>;
export declare function getGcpIamCredentialRequestToJSON(
  getGcpIamCredentialRequest: GetGcpIamCredentialRequest,
): string;
//# sourceMappingURL=getgcpiamcredential.d.ts.map
