import * as z from "zod/v4-mini";
export type CreditUsageSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreditUsageRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type CreditUsageSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreditUsageSecurity$outboundSchema: z.ZodMiniType<
  CreditUsageSecurity$Outbound,
  CreditUsageSecurity
>;
export declare function creditUsageSecurityToJSON(
  creditUsageSecurity: CreditUsageSecurity,
): string;
/** @internal */
export type CreditUsageRequest$Outbound = {
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreditUsageRequest$outboundSchema: z.ZodMiniType<
  CreditUsageRequest$Outbound,
  CreditUsageRequest
>;
export declare function creditUsageRequestToJSON(
  creditUsageRequest: CreditUsageRequest,
): string;
//# sourceMappingURL=creditusage.d.ts.map
