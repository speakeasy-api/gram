import * as z from "zod/v4-mini";
export type GetActiveDeploymentSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetActiveDeploymentSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetActiveDeploymentSecurity = {
    option1?: GetActiveDeploymentSecurityOption1 | undefined;
    option2?: GetActiveDeploymentSecurityOption2 | undefined;
};
export type GetActiveDeploymentRequest = {
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
export type GetActiveDeploymentSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetActiveDeploymentSecurityOption1$outboundSchema: z.ZodMiniType<GetActiveDeploymentSecurityOption1$Outbound, GetActiveDeploymentSecurityOption1>;
export declare function getActiveDeploymentSecurityOption1ToJSON(getActiveDeploymentSecurityOption1: GetActiveDeploymentSecurityOption1): string;
/** @internal */
export type GetActiveDeploymentSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetActiveDeploymentSecurityOption2$outboundSchema: z.ZodMiniType<GetActiveDeploymentSecurityOption2$Outbound, GetActiveDeploymentSecurityOption2>;
export declare function getActiveDeploymentSecurityOption2ToJSON(getActiveDeploymentSecurityOption2: GetActiveDeploymentSecurityOption2): string;
/** @internal */
export type GetActiveDeploymentSecurity$Outbound = {
    Option1?: GetActiveDeploymentSecurityOption1$Outbound | undefined;
    Option2?: GetActiveDeploymentSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetActiveDeploymentSecurity$outboundSchema: z.ZodMiniType<GetActiveDeploymentSecurity$Outbound, GetActiveDeploymentSecurity>;
export declare function getActiveDeploymentSecurityToJSON(getActiveDeploymentSecurity: GetActiveDeploymentSecurity): string;
/** @internal */
export type GetActiveDeploymentRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetActiveDeploymentRequest$outboundSchema: z.ZodMiniType<GetActiveDeploymentRequest$Outbound, GetActiveDeploymentRequest>;
export declare function getActiveDeploymentRequestToJSON(getActiveDeploymentRequest: GetActiveDeploymentRequest): string;
//# sourceMappingURL=getactivedeployment.d.ts.map