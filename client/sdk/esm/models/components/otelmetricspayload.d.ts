import * as z from "zod/v4-mini";
import { OTELResourceMetrics, OTELResourceMetrics$Outbound } from "./otelresourcemetrics.js";
/**
 * OTEL metrics export payload
 */
export type OTELMetricsPayload = {
    /**
     * Array of resource metrics
     */
    resourceMetrics?: Array<OTELResourceMetrics> | undefined;
};
/** @internal */
export type OTELMetricsPayload$Outbound = {
    resourceMetrics?: Array<OTELResourceMetrics$Outbound> | undefined;
};
/** @internal */
export declare const OTELMetricsPayload$outboundSchema: z.ZodMiniType<OTELMetricsPayload$Outbound, OTELMetricsPayload>;
export declare function otelMetricsPayloadToJSON(otelMetricsPayload: OTELMetricsPayload): string;
//# sourceMappingURL=otelmetricspayload.d.ts.map