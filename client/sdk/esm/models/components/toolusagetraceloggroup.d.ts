import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Child-log lookup strategy for a tool usage trace row
 */
export declare const ToolUsageTraceLogGroupKind: {
    readonly TraceId: "trace_id";
    readonly CorrelationId: "correlation_id";
    readonly TriggerEventId: "trigger_event_id";
    readonly LogId: "log_id";
};
/**
 * Child-log lookup strategy for a tool usage trace row
 */
export type ToolUsageTraceLogGroupKind = ClosedEnum<typeof ToolUsageTraceLogGroupKind>;
/**
 * Descriptor used by the dashboard to fetch child logs for a trace row
 */
export type ToolUsageTraceLogGroup = {
    /**
     * Child-log lookup strategy for a tool usage trace row
     */
    kind: ToolUsageTraceLogGroupKind;
    /**
     * Lookup value
     */
    value: string;
};
/** @internal */
export declare const ToolUsageTraceLogGroupKind$inboundSchema: z.ZodMiniEnum<typeof ToolUsageTraceLogGroupKind>;
/** @internal */
export declare const ToolUsageTraceLogGroup$inboundSchema: z.ZodMiniType<ToolUsageTraceLogGroup, unknown>;
export declare function toolUsageTraceLogGroupFromJSON(jsonString: string): SafeParseResult<ToolUsageTraceLogGroup, SDKValidationError>;
//# sourceMappingURL=toolusagetraceloggroup.d.ts.map