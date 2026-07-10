import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Hosted MCP server filter option with usage in the selected time window
 */
export type ToolUsageHostedServerFilterOption = {
    /**
     * Number of tool usage events observed for the hosted MCP server
     */
    eventCount: number;
    /**
     * Hosted MCP toolset display name
     */
    toolsetName: string;
    /**
     * Hosted MCP toolset slug
     */
    toolsetSlug: string;
};
/** @internal */
export declare const ToolUsageHostedServerFilterOption$inboundSchema: z.ZodMiniType<ToolUsageHostedServerFilterOption, unknown>;
export declare function toolUsageHostedServerFilterOptionFromJSON(jsonString: string): SafeParseResult<ToolUsageHostedServerFilterOption, SDKValidationError>;
//# sourceMappingURL=toolusagehostedserverfilteroption.d.ts.map