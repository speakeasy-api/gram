import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AgentPlugin = {
    /**
     * Name of the marketplace this plugin lives in. Always equals the `name` of one of the marketplaces in the same response.
     */
    marketplaceName: string;
    /**
     * Plugin slug. Combined with marketplace_name, this identifies the plugin the agent enables in the managed tool.
     */
    slug: string;
};
/** @internal */
export declare const AgentPlugin$inboundSchema: z.ZodMiniType<AgentPlugin, unknown>;
export declare function agentPluginFromJSON(jsonString: string): SafeParseResult<AgentPlugin, SDKValidationError>;
//# sourceMappingURL=agentplugin.d.ts.map