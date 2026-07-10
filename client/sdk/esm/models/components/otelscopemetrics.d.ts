import * as z from "zod/v4-mini";
import { OTELMetric, OTELMetric$Outbound } from "./otelmetric.js";
import { OTELScope, OTELScope$Outbound } from "./otelscope.js";
/**
 * OTEL scope metrics container
 */
export type OTELScopeMetrics = {
    /**
     * Array of metrics
     */
    metrics?: Array<OTELMetric> | undefined;
    /**
     * OTEL instrumentation scope
     */
    scope?: OTELScope | undefined;
};
/** @internal */
export type OTELScopeMetrics$Outbound = {
    metrics?: Array<OTELMetric$Outbound> | undefined;
    scope?: OTELScope$Outbound | undefined;
};
/** @internal */
export declare const OTELScopeMetrics$outboundSchema: z.ZodMiniType<OTELScopeMetrics$Outbound, OTELScopeMetrics>;
export declare function otelScopeMetricsToJSON(otelScopeMetrics: OTELScopeMetrics): string;
//# sourceMappingURL=otelscopemetrics.d.ts.map