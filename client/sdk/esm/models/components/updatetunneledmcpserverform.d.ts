import * as z from "zod/v4-mini";
/**
 * Form for updating a tunneled MCP server source
 */
export type UpdateTunneledMcpServerForm = {
    /**
     * The ID of the tunneled MCP server to update
     */
    id: string;
    /**
     * Human-readable display name for the tunneled MCP server
     */
    name: string;
};
/** @internal */
export type UpdateTunneledMcpServerForm$Outbound = {
    id: string;
    name: string;
};
/** @internal */
export declare const UpdateTunneledMcpServerForm$outboundSchema: z.ZodMiniType<UpdateTunneledMcpServerForm$Outbound, UpdateTunneledMcpServerForm>;
export declare function updateTunneledMcpServerFormToJSON(updateTunneledMcpServerForm: UpdateTunneledMcpServerForm): string;
//# sourceMappingURL=updatetunneledmcpserverform.d.ts.map