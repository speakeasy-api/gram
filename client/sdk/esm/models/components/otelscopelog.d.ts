import * as z from "zod/v4-mini";
import { OTELLogRecord, OTELLogRecord$Outbound } from "./otellogrecord.js";
import { OTELScope, OTELScope$Outbound } from "./otelscope.js";
/**
 * OTEL scope logs container
 */
export type OTELScopeLog = {
    /**
     * Array of log records
     */
    logRecords?: Array<OTELLogRecord> | undefined;
    /**
     * OTEL instrumentation scope
     */
    scope?: OTELScope | undefined;
};
/** @internal */
export type OTELScopeLog$Outbound = {
    logRecords?: Array<OTELLogRecord$Outbound> | undefined;
    scope?: OTELScope$Outbound | undefined;
};
/** @internal */
export declare const OTELScopeLog$outboundSchema: z.ZodMiniType<OTELScopeLog$Outbound, OTELScopeLog>;
export declare function otelScopeLogToJSON(otelScopeLog: OTELScopeLog): string;
//# sourceMappingURL=otelscopelog.d.ts.map