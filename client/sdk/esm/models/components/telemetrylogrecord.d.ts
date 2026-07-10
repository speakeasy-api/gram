import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ServiceInfo } from "./serviceinfo.js";
/**
 * OpenTelemetry log record
 */
export type TelemetryLogRecord = {
    /**
     * Log attributes as JSON object
     */
    attributes: any;
    /**
     * The primary log message
     */
    body: string;
    /**
     * Log record ID
     */
    id: string;
    /**
     * Unix time in nanoseconds when event was observed (string for JS int64 precision)
     */
    observedTimeUnixNano: string;
    /**
     * Resource attributes as JSON object
     */
    resourceAttributes: any;
    /**
     * Service information
     */
    service: ServiceInfo;
    /**
     * Text representation of severity
     */
    severityText?: string | undefined;
    /**
     * W3C span ID (16 hex characters)
     */
    spanId?: string | undefined;
    /**
     * Unix time in nanoseconds when event occurred (string for JS int64 precision)
     */
    timeUnixNano: string;
    /**
     * W3C trace ID (32 hex characters)
     */
    traceId?: string | undefined;
};
/** @internal */
export declare const TelemetryLogRecord$inboundSchema: z.ZodMiniType<TelemetryLogRecord, unknown>;
export declare function telemetryLogRecordFromJSON(jsonString: string): SafeParseResult<TelemetryLogRecord, SDKValidationError>;
//# sourceMappingURL=telemetrylogrecord.d.ts.map