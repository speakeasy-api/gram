import * as z from "zod/v4-mini";
export type TriggerRiskAnalysisRequestBody = {
  /**
   * The policy ID.
   */
  id: string;
  /**
   * Cap the backfill at the most recent N unanalyzed messages. Defaults to 100 (the recent-N drain budget). Pass 0 to request a full backfill of every unanalyzed message.
   */
  limit?: number | undefined;
};
/** @internal */
export type TriggerRiskAnalysisRequestBody$Outbound = {
  id: string;
  limit: number;
};
/** @internal */
export declare const TriggerRiskAnalysisRequestBody$outboundSchema: z.ZodMiniType<
  TriggerRiskAnalysisRequestBody$Outbound,
  TriggerRiskAnalysisRequestBody
>;
export declare function triggerRiskAnalysisRequestBodyToJSON(
  triggerRiskAnalysisRequestBody: TriggerRiskAnalysisRequestBody,
): string;
//# sourceMappingURL=triggerriskanalysisrequestbody.d.ts.map
