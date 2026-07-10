import * as z from "zod/v4-mini";
export type GetRemoteSessionIssuerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetRemoteSessionIssuerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetRemoteSessionIssuerSecurity = {
    option1?: GetRemoteSessionIssuerSecurityOption1 | undefined;
    option2?: GetRemoteSessionIssuerSecurityOption2 | undefined;
};
export type GetRemoteSessionIssuerRequest = {
    /**
     * The remote_session_issuer id.
     */
    id?: string | undefined;
    /**
     * The remote_session_issuer slug.
     */
    slug?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type GetRemoteSessionIssuerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRemoteSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<GetRemoteSessionIssuerSecurityOption1$Outbound, GetRemoteSessionIssuerSecurityOption1>;
export declare function getRemoteSessionIssuerSecurityOption1ToJSON(getRemoteSessionIssuerSecurityOption1: GetRemoteSessionIssuerSecurityOption1): string;
/** @internal */
export type GetRemoteSessionIssuerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRemoteSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<GetRemoteSessionIssuerSecurityOption2$Outbound, GetRemoteSessionIssuerSecurityOption2>;
export declare function getRemoteSessionIssuerSecurityOption2ToJSON(getRemoteSessionIssuerSecurityOption2: GetRemoteSessionIssuerSecurityOption2): string;
/** @internal */
export type GetRemoteSessionIssuerSecurity$Outbound = {
    Option1?: GetRemoteSessionIssuerSecurityOption1$Outbound | undefined;
    Option2?: GetRemoteSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<GetRemoteSessionIssuerSecurity$Outbound, GetRemoteSessionIssuerSecurity>;
export declare function getRemoteSessionIssuerSecurityToJSON(getRemoteSessionIssuerSecurity: GetRemoteSessionIssuerSecurity): string;
/** @internal */
export type GetRemoteSessionIssuerRequest$Outbound = {
    id?: string | undefined;
    slug?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<GetRemoteSessionIssuerRequest$Outbound, GetRemoteSessionIssuerRequest>;
export declare function getRemoteSessionIssuerRequestToJSON(getRemoteSessionIssuerRequest: GetRemoteSessionIssuerRequest): string;
//# sourceMappingURL=getremotesessionissuer.d.ts.map