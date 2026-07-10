import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Tool usage user identity kind
 */
export declare const ToolUsageUserTimeSeriesPointUserKind: {
    readonly Email: "email";
    readonly ExternalUserId: "external_user_id";
    readonly UserId: "user_id";
    readonly Unknown: "unknown";
};
/**
 * Tool usage user identity kind
 */
export type ToolUsageUserTimeSeriesPointUserKind = ClosedEnum<typeof ToolUsageUserTimeSeriesPointUserKind>;
/**
 * A time-series bucket for one tool usage user identity
 */
export type ToolUsageUserTimeSeriesPoint = {
    /**
     * Bucket start time in Unix nanoseconds as a string for JavaScript integer safety
     */
    bucketStartNs: string;
    /**
     * Number of tool usage events in the bucket
     */
    eventCount: number;
    /**
     * Number of failed tool usage events in the bucket
     */
    failureCount: number;
    /**
     * Stable user identity value used by filters and chart grouping
     */
    userKey: string;
    /**
     * Tool usage user identity kind
     */
    userKind: ToolUsageUserTimeSeriesPointUserKind;
    /**
     * User-facing label for the user identity
     */
    userLabel: string;
};
/** @internal */
export declare const ToolUsageUserTimeSeriesPointUserKind$inboundSchema: z.ZodMiniEnum<typeof ToolUsageUserTimeSeriesPointUserKind>;
/** @internal */
export declare const ToolUsageUserTimeSeriesPoint$inboundSchema: z.ZodMiniType<ToolUsageUserTimeSeriesPoint, unknown>;
export declare function toolUsageUserTimeSeriesPointFromJSON(jsonString: string): SafeParseResult<ToolUsageUserTimeSeriesPoint, SDKValidationError>;
//# sourceMappingURL=toolusageusertimeseriespoint.d.ts.map