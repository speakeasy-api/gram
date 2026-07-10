import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ToolUsageTargetSummary } from "./toolusagetargetsummary.js";
import { ToolUsageTargetTimeSeriesPoint } from "./toolusagetargettimeseriespoint.js";
import { ToolUsageTargetToolBreakdownRow } from "./toolusagetargettoolbreakdownrow.js";
import { ToolUsageTotals } from "./toolusagetotals.js";
import { ToolUsageUsersByTargetRow } from "./toolusageusersbytargetrow.js";
import { ToolUsageUserSummary } from "./toolusageusersummary.js";
import { ToolUsageUserTimeSeriesPoint } from "./toolusageusertimeseriespoint.js";
/**
 * Target-aware MCP and tool usage metrics
 */
export type GetToolUsageSummaryResult = {
    /**
     * Time-series usage buckets grouped by target
     */
    targetTimeSeries: Array<ToolUsageTargetTimeSeriesPoint>;
    /**
     * Per-tool usage rows grouped by target
     */
    targetToolBreakdown: Array<ToolUsageTargetToolBreakdownRow>;
    /**
     * Top usage targets for the selected filters and time range
     */
    targets: Array<ToolUsageTargetSummary>;
    /**
     * Target-aware MCP and tool usage totals
     */
    totals: ToolUsageTotals;
    /**
     * Time-series usage buckets grouped by user identity
     */
    userTimeSeries: Array<ToolUsageUserTimeSeriesPoint>;
    /**
     * Top user identities for the selected filters and time range
     */
    users: Array<ToolUsageUserSummary>;
    /**
     * Cross-dimensional usage rows grouped by target and user identity
     */
    usersByTarget: Array<ToolUsageUsersByTargetRow>;
};
/** @internal */
export declare const GetToolUsageSummaryResult$inboundSchema: z.ZodMiniType<GetToolUsageSummaryResult, unknown>;
export declare function getToolUsageSummaryResultFromJSON(jsonString: string): SafeParseResult<GetToolUsageSummaryResult, SDKValidationError>;
//# sourceMappingURL=gettoolusagesummaryresult.d.ts.map