import * as z from "zod/v4-mini";
/**
 * Payload for getting project-level metrics summary
 */
export type GetProjectMetricsSummaryPayload = {
  /**
   * Start time in ISO 8601 format
   */
  from: Date;
  /**
   * End time in ISO 8601 format
   */
  to: Date;
};
/** @internal */
export type GetProjectMetricsSummaryPayload$Outbound = {
  from: string;
  to: string;
};
/** @internal */
export declare const GetProjectMetricsSummaryPayload$outboundSchema: z.ZodMiniType<
  GetProjectMetricsSummaryPayload$Outbound,
  GetProjectMetricsSummaryPayload
>;
export declare function getProjectMetricsSummaryPayloadToJSON(
  getProjectMetricsSummaryPayload: GetProjectMetricsSummaryPayload,
): string;
//# sourceMappingURL=getprojectmetricssummarypayload.d.ts.map
