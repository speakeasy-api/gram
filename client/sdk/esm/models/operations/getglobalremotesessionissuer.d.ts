import * as z from "zod/v4-mini";
export type GetGlobalRemoteSessionIssuerSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type GetGlobalRemoteSessionIssuerRequest = {
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
export type GetGlobalRemoteSessionIssuerSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetGlobalRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<
  GetGlobalRemoteSessionIssuerSecurity$Outbound,
  GetGlobalRemoteSessionIssuerSecurity
>;
export declare function getGlobalRemoteSessionIssuerSecurityToJSON(
  getGlobalRemoteSessionIssuerSecurity: GetGlobalRemoteSessionIssuerSecurity,
): string;
/** @internal */
export type GetGlobalRemoteSessionIssuerRequest$Outbound = {
  id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetGlobalRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<
  GetGlobalRemoteSessionIssuerRequest$Outbound,
  GetGlobalRemoteSessionIssuerRequest
>;
export declare function getGlobalRemoteSessionIssuerRequestToJSON(
  getGlobalRemoteSessionIssuerRequest: GetGlobalRemoteSessionIssuerRequest,
): string;
//# sourceMappingURL=getglobalremotesessionissuer.d.ts.map
