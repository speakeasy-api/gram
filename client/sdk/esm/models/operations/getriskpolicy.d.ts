import * as z from "zod/v4-mini";
export type GetRiskPolicySecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetRiskPolicySecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetRiskPolicySecurity = {
    option1?: GetRiskPolicySecurityOption1 | undefined;
    option2?: GetRiskPolicySecurityOption2 | undefined;
};
export type GetRiskPolicyRequest = {
    /**
     * The policy ID.
     */
    id: string;
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
export type GetRiskPolicySecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRiskPolicySecurityOption1$outboundSchema: z.ZodMiniType<GetRiskPolicySecurityOption1$Outbound, GetRiskPolicySecurityOption1>;
export declare function getRiskPolicySecurityOption1ToJSON(getRiskPolicySecurityOption1: GetRiskPolicySecurityOption1): string;
/** @internal */
export type GetRiskPolicySecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRiskPolicySecurityOption2$outboundSchema: z.ZodMiniType<GetRiskPolicySecurityOption2$Outbound, GetRiskPolicySecurityOption2>;
export declare function getRiskPolicySecurityOption2ToJSON(getRiskPolicySecurityOption2: GetRiskPolicySecurityOption2): string;
/** @internal */
export type GetRiskPolicySecurity$Outbound = {
    Option1?: GetRiskPolicySecurityOption1$Outbound | undefined;
    Option2?: GetRiskPolicySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRiskPolicySecurity$outboundSchema: z.ZodMiniType<GetRiskPolicySecurity$Outbound, GetRiskPolicySecurity>;
export declare function getRiskPolicySecurityToJSON(getRiskPolicySecurity: GetRiskPolicySecurity): string;
/** @internal */
export type GetRiskPolicyRequest$Outbound = {
    id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRiskPolicyRequest$outboundSchema: z.ZodMiniType<GetRiskPolicyRequest$Outbound, GetRiskPolicyRequest>;
export declare function getRiskPolicyRequestToJSON(getRiskPolicyRequest: GetRiskPolicyRequest): string;
//# sourceMappingURL=getriskpolicy.d.ts.map