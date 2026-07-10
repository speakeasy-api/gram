import * as z from "zod/v4-mini";
/**
 * OTEL instrumentation scope
 */
export type OTELScope = {
    /**
     * Scope name
     */
    name?: string | undefined;
    /**
     * Scope version
     */
    version?: string | undefined;
};
/** @internal */
export type OTELScope$Outbound = {
    name?: string | undefined;
    version?: string | undefined;
};
/** @internal */
export declare const OTELScope$outboundSchema: z.ZodMiniType<OTELScope$Outbound, OTELScope>;
export declare function otelScopeToJSON(otelScope: OTELScope): string;
//# sourceMappingURL=otelscope.d.ts.map