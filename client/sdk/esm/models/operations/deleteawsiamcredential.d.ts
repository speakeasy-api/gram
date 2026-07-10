import * as z from "zod/v4-mini";
export type DeleteAwsIamCredentialSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type DeleteAwsIamCredentialRequest = {
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
export type DeleteAwsIamCredentialSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteAwsIamCredentialSecurity$outboundSchema: z.ZodMiniType<
  DeleteAwsIamCredentialSecurity$Outbound,
  DeleteAwsIamCredentialSecurity
>;
export declare function deleteAwsIamCredentialSecurityToJSON(
  deleteAwsIamCredentialSecurity: DeleteAwsIamCredentialSecurity,
): string;
/** @internal */
export type DeleteAwsIamCredentialRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteAwsIamCredentialRequest$outboundSchema: z.ZodMiniType<
  DeleteAwsIamCredentialRequest$Outbound,
  DeleteAwsIamCredentialRequest
>;
export declare function deleteAwsIamCredentialRequestToJSON(
  deleteAwsIamCredentialRequest: DeleteAwsIamCredentialRequest,
): string;
//# sourceMappingURL=deleteawsiamcredential.d.ts.map
