import * as z from "zod/v4-mini";
import { OTELAttribute, OTELAttribute$Outbound } from "./otelattribute.js";
/**
 * OTEL number data point
 */
export type OTELNumberDataPoint = {
    /**
     * Value as double
     */
    asDouble?: number | undefined;
    /**
     * Value as integer (string-encoded per OTLP/JSON, or raw number)
     */
    asInt?: any | undefined;
    /**
     * Data point attributes
     */
    attributes?: Array<OTELAttribute> | undefined;
    /**
     * Start timestamp in nanoseconds
     */
    startTimeUnixNano?: string | undefined;
    /**
     * Timestamp in nanoseconds
     */
    timeUnixNano?: string | undefined;
};
/** @internal */
export type OTELNumberDataPoint$Outbound = {
    asDouble?: number | undefined;
    asInt?: any | undefined;
    attributes?: Array<OTELAttribute$Outbound> | undefined;
    startTimeUnixNano?: string | undefined;
    timeUnixNano?: string | undefined;
};
/** @internal */
export declare const OTELNumberDataPoint$outboundSchema: z.ZodMiniType<OTELNumberDataPoint$Outbound, OTELNumberDataPoint>;
export declare function otelNumberDataPointToJSON(otelNumberDataPoint: OTELNumberDataPoint): string;
//# sourceMappingURL=otelnumberdatapoint.d.ts.map