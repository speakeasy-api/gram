import * as z from "zod/v4-mini";
export type ListRiskExclusionsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListRiskExclusionsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListRiskExclusionsSecurity = {
    option1?: ListRiskExclusionsSecurityOption1 | undefined;
    option2?: ListRiskExclusionsSecurityOption2 | undefined;
};
export type ListRiskExclusionsRequest = {
    /**
     * Filter to exclusions bound to this policy. Omit to return all exclusions (global plus every policy).
     */
    riskPolicyId?: string | undefined;
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
export type ListRiskExclusionsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRiskExclusionsSecurityOption1$outboundSchema: z.ZodMiniType<ListRiskExclusionsSecurityOption1$Outbound, ListRiskExclusionsSecurityOption1>;
export declare function listRiskExclusionsSecurityOption1ToJSON(listRiskExclusionsSecurityOption1: ListRiskExclusionsSecurityOption1): string;
/** @internal */
export type ListRiskExclusionsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRiskExclusionsSecurityOption2$outboundSchema: z.ZodMiniType<ListRiskExclusionsSecurityOption2$Outbound, ListRiskExclusionsSecurityOption2>;
export declare function listRiskExclusionsSecurityOption2ToJSON(listRiskExclusionsSecurityOption2: ListRiskExclusionsSecurityOption2): string;
/** @internal */
export type ListRiskExclusionsSecurity$Outbound = {
    Option1?: ListRiskExclusionsSecurityOption1$Outbound | undefined;
    Option2?: ListRiskExclusionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRiskExclusionsSecurity$outboundSchema: z.ZodMiniType<ListRiskExclusionsSecurity$Outbound, ListRiskExclusionsSecurity>;
export declare function listRiskExclusionsSecurityToJSON(listRiskExclusionsSecurity: ListRiskExclusionsSecurity): string;
/** @internal */
export type ListRiskExclusionsRequest$Outbound = {
    risk_policy_id?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRiskExclusionsRequest$outboundSchema: z.ZodMiniType<ListRiskExclusionsRequest$Outbound, ListRiskExclusionsRequest>;
export declare function listRiskExclusionsRequestToJSON(listRiskExclusionsRequest: ListRiskExclusionsRequest): string;
//# sourceMappingURL=listriskexclusions.d.ts.map