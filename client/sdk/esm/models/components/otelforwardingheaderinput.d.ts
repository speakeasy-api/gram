import * as z from "zod/v4-mini";
/**
 * HTTP header value provided when upserting a forwarding config.
 */
export type OtelForwardingHeaderInput = {
    /**
     * Header name.
     */
    name: string;
    /**
     * Header value. Stored encrypted at rest; never returned on reads.
     */
    value: string;
};
/** @internal */
export type OtelForwardingHeaderInput$Outbound = {
    name: string;
    value: string;
};
/** @internal */
export declare const OtelForwardingHeaderInput$outboundSchema: z.ZodMiniType<OtelForwardingHeaderInput$Outbound, OtelForwardingHeaderInput>;
export declare function otelForwardingHeaderInputToJSON(otelForwardingHeaderInput: OtelForwardingHeaderInput): string;
//# sourceMappingURL=otelforwardingheaderinput.d.ts.map