import * as z from "zod/v4-mini";
export type CheckMCPSlugAvailabilitySecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CheckMCPSlugAvailabilitySecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CheckMCPSlugAvailabilitySecurity = {
  option1?: CheckMCPSlugAvailabilitySecurityOption1 | undefined;
  option2?: CheckMCPSlugAvailabilitySecurityOption2 | undefined;
};
export type CheckMCPSlugAvailabilityRequest = {
  /**
   * The slug to check
   */
  slug: string;
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
export type CheckMCPSlugAvailabilitySecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CheckMCPSlugAvailabilitySecurityOption1$outboundSchema: z.ZodMiniType<
  CheckMCPSlugAvailabilitySecurityOption1$Outbound,
  CheckMCPSlugAvailabilitySecurityOption1
>;
export declare function checkMCPSlugAvailabilitySecurityOption1ToJSON(
  checkMCPSlugAvailabilitySecurityOption1: CheckMCPSlugAvailabilitySecurityOption1,
): string;
/** @internal */
export type CheckMCPSlugAvailabilitySecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CheckMCPSlugAvailabilitySecurityOption2$outboundSchema: z.ZodMiniType<
  CheckMCPSlugAvailabilitySecurityOption2$Outbound,
  CheckMCPSlugAvailabilitySecurityOption2
>;
export declare function checkMCPSlugAvailabilitySecurityOption2ToJSON(
  checkMCPSlugAvailabilitySecurityOption2: CheckMCPSlugAvailabilitySecurityOption2,
): string;
/** @internal */
export type CheckMCPSlugAvailabilitySecurity$Outbound = {
  Option1?: CheckMCPSlugAvailabilitySecurityOption1$Outbound | undefined;
  Option2?: CheckMCPSlugAvailabilitySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CheckMCPSlugAvailabilitySecurity$outboundSchema: z.ZodMiniType<
  CheckMCPSlugAvailabilitySecurity$Outbound,
  CheckMCPSlugAvailabilitySecurity
>;
export declare function checkMCPSlugAvailabilitySecurityToJSON(
  checkMCPSlugAvailabilitySecurity: CheckMCPSlugAvailabilitySecurity,
): string;
/** @internal */
export type CheckMCPSlugAvailabilityRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const CheckMCPSlugAvailabilityRequest$outboundSchema: z.ZodMiniType<
  CheckMCPSlugAvailabilityRequest$Outbound,
  CheckMCPSlugAvailabilityRequest
>;
export declare function checkMCPSlugAvailabilityRequestToJSON(
  checkMCPSlugAvailabilityRequest: CheckMCPSlugAvailabilityRequest,
): string;
//# sourceMappingURL=checkmcpslugavailability.d.ts.map
