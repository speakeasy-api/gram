import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { LogFilter, LogFilter$Outbound } from "./logfilter.js";
import { ToolUsageUserFilter, ToolUsageUserFilter$Outbound } from "./toolusageuserfilter.js";
/**
 * Sort order
 */
export declare const ListToolUsageTracesPayloadSort: {
    readonly Asc: "asc";
    readonly Desc: "desc";
};
/**
 * Sort order
 */
export type ListToolUsageTracesPayloadSort = ClosedEnum<typeof ListToolUsageTracesPayloadSort>;
/**
 * Tool usage target type
 */
export declare const ListToolUsageTracesPayloadTargetTypes: {
    readonly HostedMcpServer: "hosted_mcp_server";
    readonly TunneledMcpServer: "tunneled_mcp_server";
    readonly ShadowMcpServer: "shadow_mcp_server";
    readonly LocalTool: "local_tool";
    readonly Skill: "skill";
};
/**
 * Tool usage target type
 */
export type ListToolUsageTracesPayloadTargetTypes = ClosedEnum<typeof ListToolUsageTracesPayloadTargetTypes>;
/**
 * Payload for listing target-aware MCP and tool usage traces
 */
export type ListToolUsageTracesPayload = {
    /**
     * Optional account type filter ('team' or 'personal'). 'team' includes unclassified traces.
     */
    accountType?: string | undefined;
    /**
     * Cursor for pagination
     */
    cursor?: string | undefined;
    /**
     * Arbitrary attribute filter conditions from the af URL param
     */
    filters?: Array<LogFilter> | undefined;
    /**
     * Start time in ISO 8601 format
     */
    from: Date;
    /**
     * Hook plugin sources to include. Direct hosted MCP calls have no hook source and are excluded when this filter is set.
     */
    hookSources?: Array<string> | undefined;
    /**
     * Hosted MCP toolset slugs to include
     */
    hostedToolsetSlugs?: Array<string> | undefined;
    /**
     * Number of traces to return
     */
    limit?: number | undefined;
    /**
     * Free-text attribute search string from the q URL param. Matches useful identifier attributes such as Gram URN, conversation ID, and trigger instance ID.
     */
    query?: string | undefined;
    /**
     * Shadow MCP server names to include
     */
    shadowServerNames?: Array<string> | undefined;
    /**
     * Sort order
     */
    sort?: ListToolUsageTracesPayloadSort | undefined;
    /**
     * Target types to include. Empty means all target types.
     */
    targetTypes?: Array<ListToolUsageTracesPayloadTargetTypes> | undefined;
    /**
     * End time in ISO 8601 format
     */
    to: Date;
    /**
     * Typed user identities to include
     */
    userFilters?: Array<ToolUsageUserFilter> | undefined;
};
/** @internal */
export declare const ListToolUsageTracesPayloadSort$outboundSchema: z.ZodMiniEnum<typeof ListToolUsageTracesPayloadSort>;
/** @internal */
export declare const ListToolUsageTracesPayloadTargetTypes$outboundSchema: z.ZodMiniEnum<typeof ListToolUsageTracesPayloadTargetTypes>;
/** @internal */
export type ListToolUsageTracesPayload$Outbound = {
    account_type?: string | undefined;
    cursor?: string | undefined;
    filters?: Array<LogFilter$Outbound> | undefined;
    from: string;
    hook_sources?: Array<string> | undefined;
    hosted_toolset_slugs?: Array<string> | undefined;
    limit: number;
    query?: string | undefined;
    shadow_server_names?: Array<string> | undefined;
    sort: string;
    target_types?: Array<string> | undefined;
    to: string;
    user_filters?: Array<ToolUsageUserFilter$Outbound> | undefined;
};
/** @internal */
export declare const ListToolUsageTracesPayload$outboundSchema: z.ZodMiniType<ListToolUsageTracesPayload$Outbound, ListToolUsageTracesPayload>;
export declare function listToolUsageTracesPayloadToJSON(listToolUsageTracesPayload: ListToolUsageTracesPayload): string;
//# sourceMappingURL=listtoolusagetracespayload.d.ts.map