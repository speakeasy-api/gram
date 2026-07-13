import * as z from "zod/v4-mini";
export type GetOnboardingStatusSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type GetOnboardingStatusRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type GetOnboardingStatusSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetOnboardingStatusSecurity$outboundSchema: z.ZodMiniType<
  GetOnboardingStatusSecurity$Outbound,
  GetOnboardingStatusSecurity
>;
export declare function getOnboardingStatusSecurityToJSON(
  getOnboardingStatusSecurity: GetOnboardingStatusSecurity,
): string;
/** @internal */
export type GetOnboardingStatusRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetOnboardingStatusRequest$outboundSchema: z.ZodMiniType<
  GetOnboardingStatusRequest$Outbound,
  GetOnboardingStatusRequest
>;
export declare function getOnboardingStatusRequestToJSON(
  getOnboardingStatusRequest: GetOnboardingStatusRequest,
): string;
//# sourceMappingURL=getonboardingstatus.d.ts.map
