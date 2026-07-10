import * as z from "zod/v4-mini";
export type GetRiskRuleBreakdownSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetRiskRuleBreakdownSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetRiskRuleBreakdownSecurity = {
    option1?: GetRiskRuleBreakdownSecurityOption1 | undefined;
    option2?: GetRiskRuleBreakdownSecurityOption2 | undefined;
};
export type GetRiskRuleBreakdownRequest = {
    /**
     * Required category key to break down by rule_id (e.g. secrets, pii).
     */
    category: string;
    /**
     * Inclusive start of the window. Defaults to the same 7-day window as the overview.
     */
    from?: Date | undefined;
    /**
     * Exclusive end of the window. Defaults to now.
     */
    to?: Date | undefined;
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
export type GetRiskRuleBreakdownSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRiskRuleBreakdownSecurityOption1$outboundSchema: z.ZodMiniType<GetRiskRuleBreakdownSecurityOption1$Outbound, GetRiskRuleBreakdownSecurityOption1>;
export declare function getRiskRuleBreakdownSecurityOption1ToJSON(getRiskRuleBreakdownSecurityOption1: GetRiskRuleBreakdownSecurityOption1): string;
/** @internal */
export type GetRiskRuleBreakdownSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRiskRuleBreakdownSecurityOption2$outboundSchema: z.ZodMiniType<GetRiskRuleBreakdownSecurityOption2$Outbound, GetRiskRuleBreakdownSecurityOption2>;
export declare function getRiskRuleBreakdownSecurityOption2ToJSON(getRiskRuleBreakdownSecurityOption2: GetRiskRuleBreakdownSecurityOption2): string;
/** @internal */
export type GetRiskRuleBreakdownSecurity$Outbound = {
    Option1?: GetRiskRuleBreakdownSecurityOption1$Outbound | undefined;
    Option2?: GetRiskRuleBreakdownSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRiskRuleBreakdownSecurity$outboundSchema: z.ZodMiniType<GetRiskRuleBreakdownSecurity$Outbound, GetRiskRuleBreakdownSecurity>;
export declare function getRiskRuleBreakdownSecurityToJSON(getRiskRuleBreakdownSecurity: GetRiskRuleBreakdownSecurity): string;
/** @internal */
export type GetRiskRuleBreakdownRequest$Outbound = {
    category: string;
    from?: string | undefined;
    to?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRiskRuleBreakdownRequest$outboundSchema: z.ZodMiniType<GetRiskRuleBreakdownRequest$Outbound, GetRiskRuleBreakdownRequest>;
export declare function getRiskRuleBreakdownRequestToJSON(getRiskRuleBreakdownRequest: GetRiskRuleBreakdownRequest): string;
//# sourceMappingURL=getriskrulebreakdown.d.ts.map