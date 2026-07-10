import * as z from "zod/v4-mini";
export type ListShadowMCPApprovalsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListShadowMCPApprovalsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListShadowMCPApprovalsSecurity = {
    option1?: ListShadowMCPApprovalsSecurityOption1 | undefined;
    option2?: ListShadowMCPApprovalsSecurityOption2 | undefined;
};
export type ListShadowMCPApprovalsRequest = {
    /**
     * The risk policy ID.
     */
    policyId: string;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type ListShadowMCPApprovalsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListShadowMCPApprovalsSecurityOption1$outboundSchema: z.ZodMiniType<ListShadowMCPApprovalsSecurityOption1$Outbound, ListShadowMCPApprovalsSecurityOption1>;
export declare function listShadowMCPApprovalsSecurityOption1ToJSON(listShadowMCPApprovalsSecurityOption1: ListShadowMCPApprovalsSecurityOption1): string;
/** @internal */
export type ListShadowMCPApprovalsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListShadowMCPApprovalsSecurityOption2$outboundSchema: z.ZodMiniType<ListShadowMCPApprovalsSecurityOption2$Outbound, ListShadowMCPApprovalsSecurityOption2>;
export declare function listShadowMCPApprovalsSecurityOption2ToJSON(listShadowMCPApprovalsSecurityOption2: ListShadowMCPApprovalsSecurityOption2): string;
/** @internal */
export type ListShadowMCPApprovalsSecurity$Outbound = {
    Option1?: ListShadowMCPApprovalsSecurityOption1$Outbound | undefined;
    Option2?: ListShadowMCPApprovalsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListShadowMCPApprovalsSecurity$outboundSchema: z.ZodMiniType<ListShadowMCPApprovalsSecurity$Outbound, ListShadowMCPApprovalsSecurity>;
export declare function listShadowMCPApprovalsSecurityToJSON(listShadowMCPApprovalsSecurity: ListShadowMCPApprovalsSecurity): string;
/** @internal */
export type ListShadowMCPApprovalsRequest$Outbound = {
    policy_id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListShadowMCPApprovalsRequest$outboundSchema: z.ZodMiniType<ListShadowMCPApprovalsRequest$Outbound, ListShadowMCPApprovalsRequest>;
export declare function listShadowMCPApprovalsRequestToJSON(listShadowMCPApprovalsRequest: ListShadowMCPApprovalsRequest): string;
//# sourceMappingURL=listshadowmcpapprovals.d.ts.map