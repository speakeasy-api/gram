import * as z from "zod/v4-mini";
export type GetOrganizationSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type GetOrganizationRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type GetOrganizationSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationSecurity$outboundSchema: z.ZodMiniType<
  GetOrganizationSecurity$Outbound,
  GetOrganizationSecurity
>;
export declare function getOrganizationSecurityToJSON(
  getOrganizationSecurity: GetOrganizationSecurity,
): string;
/** @internal */
export type GetOrganizationRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRequest$outboundSchema: z.ZodMiniType<
  GetOrganizationRequest$Outbound,
  GetOrganizationRequest
>;
export declare function getOrganizationRequestToJSON(
  getOrganizationRequest: GetOrganizationRequest,
): string;
//# sourceMappingURL=getorganization.d.ts.map
