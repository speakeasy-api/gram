import * as z from "zod/v4-mini";
import { OTELResource, OTELResource$Outbound } from "./otelresource.js";
import { OTELScopeMetrics, OTELScopeMetrics$Outbound } from "./otelscopemetrics.js";
/**
 * OTEL resource metrics container
 */
export type OTELResourceMetrics = {
    /**
     * OTEL resource information
     */
    resource?: OTELResource | undefined;
    /**
     * Array of scope metrics
     */
    scopeMetrics?: Array<OTELScopeMetrics> | undefined;
};
/** @internal */
export type OTELResourceMetrics$Outbound = {
    resource?: OTELResource$Outbound | undefined;
    scopeMetrics?: Array<OTELScopeMetrics$Outbound> | undefined;
};
/** @internal */
export declare const OTELResourceMetrics$outboundSchema: z.ZodMiniType<OTELResourceMetrics$Outbound, OTELResourceMetrics>;
export declare function otelResourceMetricsToJSON(otelResourceMetrics: OTELResourceMetrics): string;
//# sourceMappingURL=otelresourcemetrics.d.ts.map