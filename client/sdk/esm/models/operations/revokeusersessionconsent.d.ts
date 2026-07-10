import * as z from "zod/v4-mini";
export type RevokeUserSessionConsentSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RevokeUserSessionConsentSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RevokeUserSessionConsentSecurity = {
    option1?: RevokeUserSessionConsentSecurityOption1 | undefined;
    option2?: RevokeUserSessionConsentSecurityOption2 | undefined;
};
export type RevokeUserSessionConsentRequest = {
    /**
     * The user_session_consent id.
     */
    id: string;
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
export type RevokeUserSessionConsentSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RevokeUserSessionConsentSecurityOption1$outboundSchema: z.ZodMiniType<RevokeUserSessionConsentSecurityOption1$Outbound, RevokeUserSessionConsentSecurityOption1>;
export declare function revokeUserSessionConsentSecurityOption1ToJSON(revokeUserSessionConsentSecurityOption1: RevokeUserSessionConsentSecurityOption1): string;
/** @internal */
export type RevokeUserSessionConsentSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RevokeUserSessionConsentSecurityOption2$outboundSchema: z.ZodMiniType<RevokeUserSessionConsentSecurityOption2$Outbound, RevokeUserSessionConsentSecurityOption2>;
export declare function revokeUserSessionConsentSecurityOption2ToJSON(revokeUserSessionConsentSecurityOption2: RevokeUserSessionConsentSecurityOption2): string;
/** @internal */
export type RevokeUserSessionConsentSecurity$Outbound = {
    Option1?: RevokeUserSessionConsentSecurityOption1$Outbound | undefined;
    Option2?: RevokeUserSessionConsentSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RevokeUserSessionConsentSecurity$outboundSchema: z.ZodMiniType<RevokeUserSessionConsentSecurity$Outbound, RevokeUserSessionConsentSecurity>;
export declare function revokeUserSessionConsentSecurityToJSON(revokeUserSessionConsentSecurity: RevokeUserSessionConsentSecurity): string;
/** @internal */
export type RevokeUserSessionConsentRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const RevokeUserSessionConsentRequest$outboundSchema: z.ZodMiniType<RevokeUserSessionConsentRequest$Outbound, RevokeUserSessionConsentRequest>;
export declare function revokeUserSessionConsentRequestToJSON(revokeUserSessionConsentRequest: RevokeUserSessionConsentRequest): string;
//# sourceMappingURL=revokeusersessionconsent.d.ts.map