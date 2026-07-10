import * as z from "zod/v4-mini";
/**
 * Form for rotating a tunneled MCP server source key
 */
export type RotateTunneledMcpServerKeyForm = {
    /**
     * The ID of the tunneled MCP server
     */
    id: string;
};
/** @internal */
export type RotateTunneledMcpServerKeyForm$Outbound = {
    id: string;
};
/** @internal */
export declare const RotateTunneledMcpServerKeyForm$outboundSchema: z.ZodMiniType<RotateTunneledMcpServerKeyForm$Outbound, RotateTunneledMcpServerKeyForm>;
export declare function rotateTunneledMcpServerKeyFormToJSON(rotateTunneledMcpServerKeyForm: RotateTunneledMcpServerKeyForm): string;
//# sourceMappingURL=rotatetunneledmcpserverkeyform.d.ts.map