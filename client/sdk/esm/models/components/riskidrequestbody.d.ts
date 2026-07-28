import * as z from "zod/v4-mini";
export type RiskIDRequestBody = {
  /**
   * The resource ID.
   */
  id: string;
};
/** @internal */
export type RiskIDRequestBody$Outbound = {
  id: string;
};
/** @internal */
export declare const RiskIDRequestBody$outboundSchema: z.ZodMiniType<
  RiskIDRequestBody$Outbound,
  RiskIDRequestBody
>;
export declare function riskIDRequestBodyToJSON(
  riskIDRequestBody: RiskIDRequestBody,
): string;
//# sourceMappingURL=riskidrequestbody.d.ts.map
