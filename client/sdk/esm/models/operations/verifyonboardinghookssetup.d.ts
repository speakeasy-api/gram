import * as z from "zod/v4-mini";
export type VerifyOnboardingHooksSetupSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type VerifyOnboardingHooksSetupRequest = {
  /**
   * Only return events with time_unix_nano greater than this value. Pass the previous response's latest_unix_nano to poll for new events. Stringified to preserve int64 precision.
   */
  sinceUnixNano?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type VerifyOnboardingHooksSetupSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const VerifyOnboardingHooksSetupSecurity$outboundSchema: z.ZodMiniType<
  VerifyOnboardingHooksSetupSecurity$Outbound,
  VerifyOnboardingHooksSetupSecurity
>;
export declare function verifyOnboardingHooksSetupSecurityToJSON(
  verifyOnboardingHooksSetupSecurity: VerifyOnboardingHooksSetupSecurity,
): string;
/** @internal */
export type VerifyOnboardingHooksSetupRequest$Outbound = {
  since_unix_nano?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const VerifyOnboardingHooksSetupRequest$outboundSchema: z.ZodMiniType<
  VerifyOnboardingHooksSetupRequest$Outbound,
  VerifyOnboardingHooksSetupRequest
>;
export declare function verifyOnboardingHooksSetupRequestToJSON(
  verifyOnboardingHooksSetupRequest: VerifyOnboardingHooksSetupRequest,
): string;
//# sourceMappingURL=verifyonboardinghookssetup.d.ts.map
