import * as z from "zod/v4-mini";
export type GetCustomDetectionRuleSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetCustomDetectionRuleSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetCustomDetectionRuleSecurity = {
    option1?: GetCustomDetectionRuleSecurityOption1 | undefined;
    option2?: GetCustomDetectionRuleSecurityOption2 | undefined;
};
export type GetCustomDetectionRuleRequest = {
    /**
     * The custom detection rule ID.
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
export type GetCustomDetectionRuleSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetCustomDetectionRuleSecurityOption1$outboundSchema: z.ZodMiniType<GetCustomDetectionRuleSecurityOption1$Outbound, GetCustomDetectionRuleSecurityOption1>;
export declare function getCustomDetectionRuleSecurityOption1ToJSON(getCustomDetectionRuleSecurityOption1: GetCustomDetectionRuleSecurityOption1): string;
/** @internal */
export type GetCustomDetectionRuleSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetCustomDetectionRuleSecurityOption2$outboundSchema: z.ZodMiniType<GetCustomDetectionRuleSecurityOption2$Outbound, GetCustomDetectionRuleSecurityOption2>;
export declare function getCustomDetectionRuleSecurityOption2ToJSON(getCustomDetectionRuleSecurityOption2: GetCustomDetectionRuleSecurityOption2): string;
/** @internal */
export type GetCustomDetectionRuleSecurity$Outbound = {
    Option1?: GetCustomDetectionRuleSecurityOption1$Outbound | undefined;
    Option2?: GetCustomDetectionRuleSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetCustomDetectionRuleSecurity$outboundSchema: z.ZodMiniType<GetCustomDetectionRuleSecurity$Outbound, GetCustomDetectionRuleSecurity>;
export declare function getCustomDetectionRuleSecurityToJSON(getCustomDetectionRuleSecurity: GetCustomDetectionRuleSecurity): string;
/** @internal */
export type GetCustomDetectionRuleRequest$Outbound = {
    id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetCustomDetectionRuleRequest$outboundSchema: z.ZodMiniType<GetCustomDetectionRuleRequest$Outbound, GetCustomDetectionRuleRequest>;
export declare function getCustomDetectionRuleRequestToJSON(getCustomDetectionRuleRequest: GetCustomDetectionRuleRequest): string;
//# sourceMappingURL=getcustomdetectionrule.d.ts.map