import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export declare const Policy: {
    readonly Required: "required";
    readonly Optional: "optional";
};
export type Policy = ClosedEnum<typeof Policy>;
export type AddPluginServerForm = {
    /**
     * Display name for the server. Defaults to the backing toolset or mcp_server name when omitted.
     */
    displayName?: string | undefined;
    /**
     * Gram MCP server ID for a Remote MCP-backed server. Provide exactly one of toolset_id or mcp_server_id.
     */
    mcpServerId?: string | undefined;
    pluginId: string;
    policy?: Policy | undefined;
    sortOrder?: number | undefined;
    /**
     * Gram toolset ID for a toolset-backed MCP server. Provide exactly one of toolset_id or mcp_server_id.
     */
    toolsetId?: string | undefined;
};
/** @internal */
export declare const Policy$outboundSchema: z.ZodMiniEnum<typeof Policy>;
/** @internal */
export type AddPluginServerForm$Outbound = {
    display_name?: string | undefined;
    mcp_server_id?: string | undefined;
    plugin_id: string;
    policy: string;
    sort_order: number;
    toolset_id?: string | undefined;
};
/** @internal */
export declare const AddPluginServerForm$outboundSchema: z.ZodMiniType<AddPluginServerForm$Outbound, AddPluginServerForm>;
export declare function addPluginServerFormToJSON(addPluginServerForm: AddPluginServerForm): string;
//# sourceMappingURL=addpluginserverform.d.ts.map