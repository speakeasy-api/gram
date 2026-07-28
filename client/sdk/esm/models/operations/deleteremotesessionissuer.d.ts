import * as z from "zod/v4-mini";
export type DeleteRemoteSessionIssuerSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DeleteRemoteSessionIssuerSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DeleteRemoteSessionIssuerSecurity = {
  option1?: DeleteRemoteSessionIssuerSecurityOption1 | undefined;
  option2?: DeleteRemoteSessionIssuerSecurityOption2 | undefined;
};
export type DeleteRemoteSessionIssuerRequest = {
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
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type DeleteRemoteSessionIssuerSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteRemoteSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<
  DeleteRemoteSessionIssuerSecurityOption1$Outbound,
  DeleteRemoteSessionIssuerSecurityOption1
>;
export declare function deleteRemoteSessionIssuerSecurityOption1ToJSON(
  deleteRemoteSessionIssuerSecurityOption1: DeleteRemoteSessionIssuerSecurityOption1,
): string;
/** @internal */
export type DeleteRemoteSessionIssuerSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteRemoteSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<
  DeleteRemoteSessionIssuerSecurityOption2$Outbound,
  DeleteRemoteSessionIssuerSecurityOption2
>;
export declare function deleteRemoteSessionIssuerSecurityOption2ToJSON(
  deleteRemoteSessionIssuerSecurityOption2: DeleteRemoteSessionIssuerSecurityOption2,
): string;
/** @internal */
export type DeleteRemoteSessionIssuerSecurity$Outbound = {
  Option1?: DeleteRemoteSessionIssuerSecurityOption1$Outbound | undefined;
  Option2?: DeleteRemoteSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  DeleteRemoteSessionIssuerSecurity$Outbound,
  DeleteRemoteSessionIssuerSecurity
>;
export declare function deleteRemoteSessionIssuerSecurityToJSON(
  deleteRemoteSessionIssuerSecurity: DeleteRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type DeleteRemoteSessionIssuerRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  DeleteRemoteSessionIssuerRequest$Outbound,
  DeleteRemoteSessionIssuerRequest
>;
export declare function deleteRemoteSessionIssuerRequestToJSON(
  deleteRemoteSessionIssuerRequest: DeleteRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=deleteremotesessionissuer.d.ts.map
