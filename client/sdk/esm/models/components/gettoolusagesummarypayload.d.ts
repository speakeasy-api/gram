import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { ToolUsageUserFilter, ToolUsageUserFilter$Outbound } from "./toolusageuserfilter.js";
/**
 * Tool usage target type
 */
export declare const TargetTypes: {
    readonly HostedMcpServer: "hosted_mcp_server";
    readonly TunneledMcpServer: "tunneled_mcp_server";
    readonly ShadowMcpServer: "shadow_mcp_server";
    readonly LocalTool: "local_tool";
    readonly Skill: "skill";
};
/**
 * Tool usage target type
 */
export type TargetTypes = ClosedEnum<typeof TargetTypes>;
/**
 * Payload for target-aware MCP and tool usage metrics
 */
export type GetToolUsageSummaryPayload = {
    /**
     * Optional account type filter ('team' or 'personal').
     */
    accountType?: string | undefined;
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
     * Shadow MCP server names to include
     */
    shadowServerNames?: Array<string> | undefined;
    /**
     * Target types to include. Empty means all target types.
     */
    targetTypes?: Array<TargetTypes> | undefined;
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
export declare const TargetTypes$outboundSchema: z.ZodMiniEnum<typeof TargetTypes>;
/** @internal */
export type GetToolUsageSummaryPayload$Outbound = {
    account_type?: string | undefined;
    from: string;
    hook_sources?: Array<string> | undefined;
    hosted_toolset_slugs?: Array<string> | undefined;
    shadow_server_names?: Array<string> | undefined;
    target_types?: Array<string> | undefined;
    to: string;
    user_filters?: Array<ToolUsageUserFilter$Outbound> | undefined;
};
/** @internal */
export declare const GetToolUsageSummaryPayload$outboundSchema: z.ZodMiniType<GetToolUsageSummaryPayload$Outbound, GetToolUsageSummaryPayload>;
export declare function getToolUsageSummaryPayloadToJSON(getToolUsageSummaryPayload: GetToolUsageSummaryPayload): string;
//# sourceMappingURL=gettoolusagesummarypayload.d.ts.map