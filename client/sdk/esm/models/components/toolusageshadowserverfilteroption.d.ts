import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Shadow MCP server filter option with usage in the selected time window
 */
export type ToolUsageShadowServerFilterOption = {
    /**
     * Number of tool usage events observed for the Shadow MCP server
     */
    eventCount: number;
    /**
     * Observed Shadow MCP server name
     */
    serverName: string;
};
/** @internal */
export declare const ToolUsageShadowServerFilterOption$inboundSchema: z.ZodMiniType<ToolUsageShadowServerFilterOption, unknown>;
export declare function toolUsageShadowServerFilterOptionFromJSON(jsonString: string): SafeParseResult<ToolUsageShadowServerFilterOption, SDKValidationError>;
//# sourceMappingURL=toolusageshadowserverfilteroption.d.ts.map