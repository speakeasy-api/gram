import * as z from "zod/v4-mini";
export type ListRiskPoliciesSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListRiskPoliciesSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListRiskPoliciesSecurity = {
    option1?: ListRiskPoliciesSecurityOption1 | undefined;
    option2?: ListRiskPoliciesSecurityOption2 | undefined;
};
export type ListRiskPoliciesRequest = {
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
export type ListRiskPoliciesSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRiskPoliciesSecurityOption1$outboundSchema: z.ZodMiniType<ListRiskPoliciesSecurityOption1$Outbound, ListRiskPoliciesSecurityOption1>;
export declare function listRiskPoliciesSecurityOption1ToJSON(listRiskPoliciesSecurityOption1: ListRiskPoliciesSecurityOption1): string;
/** @internal */
export type ListRiskPoliciesSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRiskPoliciesSecurityOption2$outboundSchema: z.ZodMiniType<ListRiskPoliciesSecurityOption2$Outbound, ListRiskPoliciesSecurityOption2>;
export declare function listRiskPoliciesSecurityOption2ToJSON(listRiskPoliciesSecurityOption2: ListRiskPoliciesSecurityOption2): string;
/** @internal */
export type ListRiskPoliciesSecurity$Outbound = {
    Option1?: ListRiskPoliciesSecurityOption1$Outbound | undefined;
    Option2?: ListRiskPoliciesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRiskPoliciesSecurity$outboundSchema: z.ZodMiniType<ListRiskPoliciesSecurity$Outbound, ListRiskPoliciesSecurity>;
export declare function listRiskPoliciesSecurityToJSON(listRiskPoliciesSecurity: ListRiskPoliciesSecurity): string;
/** @internal */
export type ListRiskPoliciesRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRiskPoliciesRequest$outboundSchema: z.ZodMiniType<ListRiskPoliciesRequest$Outbound, ListRiskPoliciesRequest>;
export declare function listRiskPoliciesRequestToJSON(listRiskPoliciesRequest: ListRiskPoliciesRequest): string;
//# sourceMappingURL=listriskpolicies.d.ts.map