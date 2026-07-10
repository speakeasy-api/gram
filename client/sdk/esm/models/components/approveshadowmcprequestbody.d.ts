import * as z from "zod/v4-mini";
export type ApproveShadowMCPRequestBody = {
    /**
     * The MCP server identifier to approve.
     */
    match: string;
    /**
     * The risk policy ID.
     */
    policyId: string;
    /**
     * Display name of the MCP server (optional, for UI).
     */
    serverName?: string | undefined;
};
/** @internal */
export type ApproveShadowMCPRequestBody$Outbound = {
    match: string;
    policy_id: string;
    server_name?: string | undefined;
};
/** @internal */
export declare const ApproveShadowMCPRequestBody$outboundSchema: z.ZodMiniType<ApproveShadowMCPRequestBody$Outbound, ApproveShadowMCPRequestBody>;
export declare function approveShadowMCPRequestBodyToJSON(approveShadowMCPRequestBody: ApproveShadowMCPRequestBody): string;
//# sourceMappingURL=approveshadowmcprequestbody.d.ts.map