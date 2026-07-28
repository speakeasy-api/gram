import * as z from "zod/v4-mini";
export type GetDomainSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type GetDomainRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type GetDomainSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetDomainSecurity$outboundSchema: z.ZodMiniType<
  GetDomainSecurity$Outbound,
  GetDomainSecurity
>;
export declare function getDomainSecurityToJSON(
  getDomainSecurity: GetDomainSecurity,
): string;
/** @internal */
export type GetDomainRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetDomainRequest$outboundSchema: z.ZodMiniType<
  GetDomainRequest$Outbound,
  GetDomainRequest
>;
export declare function getDomainRequestToJSON(
  getDomainRequest: GetDomainRequest,
): string;
//# sourceMappingURL=getdomain.d.ts.map
