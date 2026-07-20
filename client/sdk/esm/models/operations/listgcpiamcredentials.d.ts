import * as z from "zod/v4-mini";
export type ListGcpIamCredentialsSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ListGcpIamCredentialsRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type ListGcpIamCredentialsSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListGcpIamCredentialsSecurity$outboundSchema: z.ZodMiniType<
  ListGcpIamCredentialsSecurity$Outbound,
  ListGcpIamCredentialsSecurity
>;
export declare function listGcpIamCredentialsSecurityToJSON(
  listGcpIamCredentialsSecurity: ListGcpIamCredentialsSecurity,
): string;
/** @internal */
export type ListGcpIamCredentialsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListGcpIamCredentialsRequest$outboundSchema: z.ZodMiniType<
  ListGcpIamCredentialsRequest$Outbound,
  ListGcpIamCredentialsRequest
>;
export declare function listGcpIamCredentialsRequestToJSON(
  listGcpIamCredentialsRequest: ListGcpIamCredentialsRequest,
): string;
//# sourceMappingURL=listgcpiamcredentials.d.ts.map
