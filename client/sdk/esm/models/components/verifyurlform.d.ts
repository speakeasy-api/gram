import * as z from "zod/v4-mini";
/**
 * Form for probing a remote MCP server URL
 */
export type VerifyURLForm = {
    /**
     * The transport type for the remote MCP server (e.g. streamable-http)
     */
    transportType: string;
    /**
     * The URL of the remote MCP server to probe
     */
    url: string;
};
/** @internal */
export type VerifyURLForm$Outbound = {
    transport_type: string;
    url: string;
};
/** @internal */
export declare const VerifyURLForm$outboundSchema: z.ZodMiniType<VerifyURLForm$Outbound, VerifyURLForm>;
export declare function verifyURLFormToJSON(verifyURLForm: VerifyURLForm): string;
//# sourceMappingURL=verifyurlform.d.ts.map