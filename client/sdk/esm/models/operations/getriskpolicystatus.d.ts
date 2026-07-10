import * as z from "zod/v4-mini";
export type GetRiskPolicyStatusSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetRiskPolicyStatusSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetRiskPolicyStatusSecurity = {
    option1?: GetRiskPolicyStatusSecurityOption1 | undefined;
    option2?: GetRiskPolicyStatusSecurityOption2 | undefined;
};
export type GetRiskPolicyStatusRequest = {
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
export type GetRiskPolicyStatusSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRiskPolicyStatusSecurityOption1$outboundSchema: z.ZodMiniType<GetRiskPolicyStatusSecurityOption1$Outbound, GetRiskPolicyStatusSecurityOption1>;
export declare function getRiskPolicyStatusSecurityOption1ToJSON(getRiskPolicyStatusSecurityOption1: GetRiskPolicyStatusSecurityOption1): string;
/** @internal */
export type GetRiskPolicyStatusSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRiskPolicyStatusSecurityOption2$outboundSchema: z.ZodMiniType<GetRiskPolicyStatusSecurityOption2$Outbound, GetRiskPolicyStatusSecurityOption2>;
export declare function getRiskPolicyStatusSecurityOption2ToJSON(getRiskPolicyStatusSecurityOption2: GetRiskPolicyStatusSecurityOption2): string;
/** @internal */
export type GetRiskPolicyStatusSecurity$Outbound = {
    Option1?: GetRiskPolicyStatusSecurityOption1$Outbound | undefined;
    Option2?: GetRiskPolicyStatusSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRiskPolicyStatusSecurity$outboundSchema: z.ZodMiniType<GetRiskPolicyStatusSecurity$Outbound, GetRiskPolicyStatusSecurity>;
export declare function getRiskPolicyStatusSecurityToJSON(getRiskPolicyStatusSecurity: GetRiskPolicyStatusSecurity): string;
/** @internal */
export type GetRiskPolicyStatusRequest$Outbound = {
    id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRiskPolicyStatusRequest$outboundSchema: z.ZodMiniType<GetRiskPolicyStatusRequest$Outbound, GetRiskPolicyStatusRequest>;
export declare function getRiskPolicyStatusRequestToJSON(getRiskPolicyStatusRequest: GetRiskPolicyStatusRequest): string;
//# sourceMappingURL=getriskpolicystatus.d.ts.map