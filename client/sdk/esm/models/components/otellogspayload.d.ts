import * as z from "zod/v4-mini";
import {
  OTELResourceLog,
  OTELResourceLog$Outbound,
} from "./otelresourcelog.js";
/**
 * OTEL logs export payload
 */
export type OTELLogsPayload = {
  /**
   * Array of resource logs
   */
  resourceLogs?: Array<OTELResourceLog> | undefined;
};
/** @internal */
export type OTELLogsPayload$Outbound = {
  resourceLogs?: Array<OTELResourceLog$Outbound> | undefined;
};
/** @internal */
export declare const OTELLogsPayload$outboundSchema: z.ZodMiniType<
  OTELLogsPayload$Outbound,
  OTELLogsPayload
>;
export declare function otelLogsPayloadToJSON(
  otelLogsPayload: OTELLogsPayload,
): string;
//# sourceMappingURL=otellogspayload.d.ts.map
