import * as z from "zod/v4-mini";
/**
 * OTEL log body
 */
export type OTELLogBody = {
    /**
     * String body value
     */
    stringValue?: string | undefined;
};
/** @internal */
export type OTELLogBody$Outbound = {
    stringValue?: string | undefined;
};
/** @internal */
export declare const OTELLogBody$outboundSchema: z.ZodMiniType<OTELLogBody$Outbound, OTELLogBody>;
export declare function otelLogBodyToJSON(otelLogBody: OTELLogBody): string;
//# sourceMappingURL=otellogbody.d.ts.map