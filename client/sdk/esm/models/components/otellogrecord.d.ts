import * as z from "zod/v4-mini";
import { OTELAttribute, OTELAttribute$Outbound } from "./otelattribute.js";
import { OTELLogBody, OTELLogBody$Outbound } from "./otellogbody.js";
/**
 * Individual OTEL log record
 */
export type OTELLogRecord = {
  /**
   * Log attributes
   */
  attributes?: Array<OTELAttribute> | undefined;
  /**
   * OTEL log body
   */
  body?: OTELLogBody | undefined;
  /**
   * Number of dropped attributes
   */
  droppedAttributesCount?: number | undefined;
  /**
   * Observed timestamp in nanoseconds
   */
  observedTimeUnixNano?: string | undefined;
  /**
   * Span ID
   */
  spanId?: string | undefined;
  /**
   * Timestamp in nanoseconds since Unix epoch
   */
  timeUnixNano?: string | undefined;
  /**
   * Trace ID
   */
  traceId?: string | undefined;
};
/** @internal */
export type OTELLogRecord$Outbound = {
  attributes?: Array<OTELAttribute$Outbound> | undefined;
  body?: OTELLogBody$Outbound | undefined;
  droppedAttributesCount?: number | undefined;
  observedTimeUnixNano?: string | undefined;
  spanId?: string | undefined;
  timeUnixNano?: string | undefined;
  traceId?: string | undefined;
};
/** @internal */
export declare const OTELLogRecord$outboundSchema: z.ZodMiniType<
  OTELLogRecord$Outbound,
  OTELLogRecord
>;
export declare function otelLogRecordToJSON(
  otelLogRecord: OTELLogRecord,
): string;
//# sourceMappingURL=otellogrecord.d.ts.map
