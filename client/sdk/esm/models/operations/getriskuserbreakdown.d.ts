import * as z from "zod/v4-mini";
export type GetRiskUserBreakdownSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetRiskUserBreakdownSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetRiskUserBreakdownSecurity = {
    option1?: GetRiskUserBreakdownSecurityOption1 | undefined;
    option2?: GetRiskUserBreakdownSecurityOption2 | undefined;
};
export type GetRiskUserBreakdownRequest = {
    /**
     * External user identifier to scope the breakdown to.
     */
    externalUserId: string;
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
export type GetRiskUserBreakdownSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRiskUserBreakdownSecurityOption1$outboundSchema: z.ZodMiniType<GetRiskUserBreakdownSecurityOption1$Outbound, GetRiskUserBreakdownSecurityOption1>;
export declare function getRiskUserBreakdownSecurityOption1ToJSON(getRiskUserBreakdownSecurityOption1: GetRiskUserBreakdownSecurityOption1): string;
/** @internal */
export type GetRiskUserBreakdownSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRiskUserBreakdownSecurityOption2$outboundSchema: z.ZodMiniType<GetRiskUserBreakdownSecurityOption2$Outbound, GetRiskUserBreakdownSecurityOption2>;
export declare function getRiskUserBreakdownSecurityOption2ToJSON(getRiskUserBreakdownSecurityOption2: GetRiskUserBreakdownSecurityOption2): string;
/** @internal */
export type GetRiskUserBreakdownSecurity$Outbound = {
    Option1?: GetRiskUserBreakdownSecurityOption1$Outbound | undefined;
    Option2?: GetRiskUserBreakdownSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRiskUserBreakdownSecurity$outboundSchema: z.ZodMiniType<GetRiskUserBreakdownSecurity$Outbound, GetRiskUserBreakdownSecurity>;
export declare function getRiskUserBreakdownSecurityToJSON(getRiskUserBreakdownSecurity: GetRiskUserBreakdownSecurity): string;
/** @internal */
export type GetRiskUserBreakdownRequest$Outbound = {
    external_user_id: string;
    from?: string | undefined;
    to?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRiskUserBreakdownRequest$outboundSchema: z.ZodMiniType<GetRiskUserBreakdownRequest$Outbound, GetRiskUserBreakdownRequest>;
export declare function getRiskUserBreakdownRequestToJSON(getRiskUserBreakdownRequest: GetRiskUserBreakdownRequest): string;
//# sourceMappingURL=getriskuserbreakdown.d.ts.map