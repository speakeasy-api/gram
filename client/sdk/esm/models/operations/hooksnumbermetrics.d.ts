import * as z from "zod/v4-mini";
import { OTELMetricsPayload, OTELMetricsPayload$Outbound } from "../components/otelmetricspayload.js";
export type HooksNumberMetricsSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    projectSlugHeaderGramProject?: string | undefined;
};
export type HooksNumberMetricsRequest = {
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
    otelMetricsPayload: OTELMetricsPayload;
};
/** @internal */
export type HooksNumberMetricsSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const HooksNumberMetricsSecurity$outboundSchema: z.ZodMiniType<HooksNumberMetricsSecurity$Outbound, HooksNumberMetricsSecurity>;
export declare function hooksNumberMetricsSecurityToJSON(hooksNumberMetricsSecurity: HooksNumberMetricsSecurity): string;
/** @internal */
export type HooksNumberMetricsRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    OTELMetricsPayload: OTELMetricsPayload$Outbound;
};
/** @internal */
export declare const HooksNumberMetricsRequest$outboundSchema: z.ZodMiniType<HooksNumberMetricsRequest$Outbound, HooksNumberMetricsRequest>;
export declare function hooksNumberMetricsRequestToJSON(hooksNumberMetricsRequest: HooksNumberMetricsRequest): string;
//# sourceMappingURL=hooksnumbermetrics.d.ts.map