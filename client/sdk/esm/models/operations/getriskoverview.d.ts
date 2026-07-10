import * as z from "zod/v4-mini";
export type GetRiskOverviewSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetRiskOverviewSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetRiskOverviewSecurity = {
    option1?: GetRiskOverviewSecurityOption1 | undefined;
    option2?: GetRiskOverviewSecurityOption2 | undefined;
};
export type GetRiskOverviewRequest = {
    /**
     * Inclusive start of the overview window. Defaults to the start of the 7-day calendar window ending at to.
     */
    from?: Date | undefined;
    /**
     * Exclusive end of the overview window. Defaults to now.
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
export type GetRiskOverviewSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRiskOverviewSecurityOption1$outboundSchema: z.ZodMiniType<GetRiskOverviewSecurityOption1$Outbound, GetRiskOverviewSecurityOption1>;
export declare function getRiskOverviewSecurityOption1ToJSON(getRiskOverviewSecurityOption1: GetRiskOverviewSecurityOption1): string;
/** @internal */
export type GetRiskOverviewSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRiskOverviewSecurityOption2$outboundSchema: z.ZodMiniType<GetRiskOverviewSecurityOption2$Outbound, GetRiskOverviewSecurityOption2>;
export declare function getRiskOverviewSecurityOption2ToJSON(getRiskOverviewSecurityOption2: GetRiskOverviewSecurityOption2): string;
/** @internal */
export type GetRiskOverviewSecurity$Outbound = {
    Option1?: GetRiskOverviewSecurityOption1$Outbound | undefined;
    Option2?: GetRiskOverviewSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRiskOverviewSecurity$outboundSchema: z.ZodMiniType<GetRiskOverviewSecurity$Outbound, GetRiskOverviewSecurity>;
export declare function getRiskOverviewSecurityToJSON(getRiskOverviewSecurity: GetRiskOverviewSecurity): string;
/** @internal */
export type GetRiskOverviewRequest$Outbound = {
    from?: string | undefined;
    to?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRiskOverviewRequest$outboundSchema: z.ZodMiniType<GetRiskOverviewRequest$Outbound, GetRiskOverviewRequest>;
export declare function getRiskOverviewRequestToJSON(getRiskOverviewRequest: GetRiskOverviewRequest): string;
//# sourceMappingURL=getriskoverview.d.ts.map