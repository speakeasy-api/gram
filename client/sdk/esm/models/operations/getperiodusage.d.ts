import * as z from "zod/v4-mini";
export type GetPeriodUsageSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type GetPeriodUsageRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type GetPeriodUsageSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetPeriodUsageSecurity$outboundSchema: z.ZodMiniType<
  GetPeriodUsageSecurity$Outbound,
  GetPeriodUsageSecurity
>;
export declare function getPeriodUsageSecurityToJSON(
  getPeriodUsageSecurity: GetPeriodUsageSecurity,
): string;
/** @internal */
export type GetPeriodUsageRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetPeriodUsageRequest$outboundSchema: z.ZodMiniType<
  GetPeriodUsageRequest$Outbound,
  GetPeriodUsageRequest
>;
export declare function getPeriodUsageRequestToJSON(
  getPeriodUsageRequest: GetPeriodUsageRequest,
): string;
//# sourceMappingURL=getperiodusage.d.ts.map
