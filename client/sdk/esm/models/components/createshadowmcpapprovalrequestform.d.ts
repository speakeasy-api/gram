import * as z from "zod/v4-mini";
export type CreateShadowMCPApprovalRequestForm = {
    /**
     * Signed token from the Shadow MCP block response.
     */
    requestToken: string;
};
/** @internal */
export type CreateShadowMCPApprovalRequestForm$Outbound = {
    request_token: string;
};
/** @internal */
export declare const CreateShadowMCPApprovalRequestForm$outboundSchema: z.ZodMiniType<CreateShadowMCPApprovalRequestForm$Outbound, CreateShadowMCPApprovalRequestForm>;
export declare function createShadowMCPApprovalRequestFormToJSON(createShadowMCPApprovalRequestForm: CreateShadowMCPApprovalRequestForm): string;
//# sourceMappingURL=createshadowmcpapprovalrequestform.d.ts.map