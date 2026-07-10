import * as z from "zod/v4-mini";
/**
 * Payload for getting an employee-level MCP data flow graph
 */
export type GetEmployeeDataFlowGraphPayload = {
    /**
     * Optional account type filter ('team' or 'personal')
     */
    accountType?: string | undefined;
    /**
     * Optional filter to a single AI account by its provider org id; scopes the graph to that one account
     */
    externalOrgId?: string | undefined;
    /**
     * External user ID to get the graph for (mutually exclusive with user_id)
     */
    externalUserId?: string | undefined;
    /**
     * Start time in ISO 8601 format
     */
    from: Date;
    /**
     * End time in ISO 8601 format
     */
    to: Date;
    /**
     * User ID to get the graph for (mutually exclusive with external_user_id)
     */
    userId?: string | undefined;
};
/** @internal */
export type GetEmployeeDataFlowGraphPayload$Outbound = {
    account_type?: string | undefined;
    external_org_id?: string | undefined;
    external_user_id?: string | undefined;
    from: string;
    to: string;
    user_id?: string | undefined;
};
/** @internal */
export declare const GetEmployeeDataFlowGraphPayload$outboundSchema: z.ZodMiniType<GetEmployeeDataFlowGraphPayload$Outbound, GetEmployeeDataFlowGraphPayload>;
export declare function getEmployeeDataFlowGraphPayloadToJSON(getEmployeeDataFlowGraphPayload: GetEmployeeDataFlowGraphPayload): string;
//# sourceMappingURL=getemployeedataflowgraphpayload.d.ts.map