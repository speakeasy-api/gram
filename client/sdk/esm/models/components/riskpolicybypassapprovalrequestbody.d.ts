import * as z from "zod/v4-mini";
export type RiskPolicyBypassApprovalRequestBody = {
    /**
     * Principal URNs to grant bypass access to. Use user:all for every user in the organization.
     */
    grantedPrincipalUrns?: Array<string> | undefined;
    /**
     * The bypass request ID.
     */
    id: string;
};
/** @internal */
export type RiskPolicyBypassApprovalRequestBody$Outbound = {
    granted_principal_urns?: Array<string> | undefined;
    id: string;
};
/** @internal */
export declare const RiskPolicyBypassApprovalRequestBody$outboundSchema: z.ZodMiniType<RiskPolicyBypassApprovalRequestBody$Outbound, RiskPolicyBypassApprovalRequestBody>;
export declare function riskPolicyBypassApprovalRequestBodyToJSON(riskPolicyBypassApprovalRequestBody: RiskPolicyBypassApprovalRequestBody): string;
//# sourceMappingURL=riskpolicybypassapprovalrequestbody.d.ts.map