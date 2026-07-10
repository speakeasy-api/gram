import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { HooksBreakdownRow } from "./hooksbreakdownrow.js";
import { HooksServerSummary } from "./hooksserversummary.js";
import { HooksTimeSeriesPoint } from "./hookstimeseriespoint.js";
import { HooksUserSummary } from "./hooksusersummary.js";
import { SkillBreakdownRow } from "./skillbreakdownrow.js";
import { SkillSummary } from "./skillsummary.js";
import { SkillTimeSeriesPoint } from "./skilltimeseriespoint.js";
/**
 * Result of hooks summary query
 */
export type GetHooksSummaryResult = {
    /**
     * Cross-dimensional pivot: (user, server, source, tool) x counts
     */
    breakdown: Array<HooksBreakdownRow>;
    /**
     * Aggregated metrics grouped by server
     */
    servers: Array<HooksServerSummary>;
    /**
     * Per-user skill breakdown
     */
    skillBreakdown: Array<SkillBreakdownRow>;
    /**
     * Time-bucketed event counts by skill
     */
    skillTimeSeries: Array<SkillTimeSeriesPoint>;
    /**
     * Aggregated metrics grouped by skill
     */
    skills: Array<SkillSummary>;
    /**
     * Time-bucketed event counts by server and user
     */
    timeSeries: Array<HooksTimeSeriesPoint>;
    /**
     * Total number of hook events
     */
    totalEvents: number;
    /**
     * Total number of unique sessions
     */
    totalSessions: number;
    /**
     * Aggregated metrics grouped by user
     */
    users: Array<HooksUserSummary>;
};
/** @internal */
export declare const GetHooksSummaryResult$inboundSchema: z.ZodMiniType<GetHooksSummaryResult, unknown>;
export declare function getHooksSummaryResultFromJSON(jsonString: string): SafeParseResult<GetHooksSummaryResult, SDKValidationError>;
//# sourceMappingURL=gethookssummaryresult.d.ts.map