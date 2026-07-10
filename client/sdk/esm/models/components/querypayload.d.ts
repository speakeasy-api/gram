import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { QueryFilter, QueryFilter$Outbound } from "./queryfilter.js";
/**
 * Optional dimension to break results down by. When omitted, a single aggregate row/series for the whole slice is returned.
 */
export declare const GroupBy: {
    readonly DepartmentName: "department_name";
    readonly JobTitle: "job_title";
    readonly EmployeeType: "employee_type";
    readonly DivisionName: "division_name";
    readonly CostCenterName: "cost_center_name";
    readonly Email: "email";
    readonly Model: "model";
    readonly HookSource: "hook_source";
    readonly AccountType: "account_type";
    readonly Provider: "provider";
    readonly BillingMode: "billing_mode";
    readonly QuerySource: "query_source";
    readonly SkillName: "skill_name";
    readonly AgentName: "agent_name";
    readonly McpServerName: "mcp_server_name";
    readonly McpToolName: "mcp_tool_name";
    readonly Role: "role";
    readonly Group: "group";
    readonly ProjectId: "project_id";
};
/**
 * Optional dimension to break results down by. When omitted, a single aggregate row/series for the whole slice is returned.
 */
export type GroupBy = ClosedEnum<typeof GroupBy>;
/**
 * Measure used to rank groups for top_n. Defaults to total_cost.
 */
export declare const QueryPayloadSortBy: {
    readonly TotalCost: "total_cost";
    readonly TotalTokens: "total_tokens";
    readonly TotalInputTokens: "total_input_tokens";
    readonly TotalOutputTokens: "total_output_tokens";
    readonly CacheReadInputTokens: "cache_read_input_tokens";
    readonly CacheCreationInputTokens: "cache_creation_input_tokens";
    readonly TotalToolCalls: "total_tool_calls";
    readonly TotalChats: "total_chats";
};
/**
 * Measure used to rank groups for top_n. Defaults to total_cost.
 */
export type QueryPayloadSortBy = ClosedEnum<typeof QueryPayloadSortBy>;
/**
 * Payload for a generic org-scoped analytics query
 */
export type QueryPayload = {
    /**
     * Optional filters; all filters are ANDed together.
     */
    filters?: Array<QueryFilter> | undefined;
    /**
     * Start time in ISO 8601 format
     */
    from: Date;
    /**
     * Optional timeseries bucket size in seconds. Defaults to an interval derived from the time range and is floored to 3600 (the source data is bucketed hourly).
     */
    granularitySeconds?: number | undefined;
    /**
     * Optional dimension to break results down by. When omitted, a single aggregate row/series for the whole slice is returned.
     */
    groupBy?: GroupBy | undefined;
    /**
     * Measure used to rank groups for top_n. Defaults to total_cost.
     */
    sortBy?: QueryPayloadSortBy | undefined;
    /**
     * End time in ISO 8601 format
     */
    to: Date;
    /**
     * When group_by is set, keep at most this many groups (ranked by sort_by); the remainder are rolled into an 'Other' group. Defaults to 10.
     */
    topN?: number | undefined;
};
/** @internal */
export declare const GroupBy$outboundSchema: z.ZodMiniEnum<typeof GroupBy>;
/** @internal */
export declare const QueryPayloadSortBy$outboundSchema: z.ZodMiniEnum<typeof QueryPayloadSortBy>;
/** @internal */
export type QueryPayload$Outbound = {
    filters?: Array<QueryFilter$Outbound> | undefined;
    from: string;
    granularity_seconds?: number | undefined;
    group_by?: string | undefined;
    sort_by: string;
    to: string;
    top_n: number;
};
/** @internal */
export declare const QueryPayload$outboundSchema: z.ZodMiniType<QueryPayload$Outbound, QueryPayload>;
export declare function queryPayloadToJSON(queryPayload: QueryPayload): string;
//# sourceMappingURL=querypayload.d.ts.map