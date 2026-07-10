import * as z from "zod/v4-mini";
export type RevokeUserSessionSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RevokeUserSessionSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RevokeUserSessionSecurity = {
    option1?: RevokeUserSessionSecurityOption1 | undefined;
    option2?: RevokeUserSessionSecurityOption2 | undefined;
};
export type RevokeUserSessionRequest = {
    /**
     * The user_session id.
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
export type RevokeUserSessionSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RevokeUserSessionSecurityOption1$outboundSchema: z.ZodMiniType<RevokeUserSessionSecurityOption1$Outbound, RevokeUserSessionSecurityOption1>;
export declare function revokeUserSessionSecurityOption1ToJSON(revokeUserSessionSecurityOption1: RevokeUserSessionSecurityOption1): string;
/** @internal */
export type RevokeUserSessionSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RevokeUserSessionSecurityOption2$outboundSchema: z.ZodMiniType<RevokeUserSessionSecurityOption2$Outbound, RevokeUserSessionSecurityOption2>;
export declare function revokeUserSessionSecurityOption2ToJSON(revokeUserSessionSecurityOption2: RevokeUserSessionSecurityOption2): string;
/** @internal */
export type RevokeUserSessionSecurity$Outbound = {
    Option1?: RevokeUserSessionSecurityOption1$Outbound | undefined;
    Option2?: RevokeUserSessionSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RevokeUserSessionSecurity$outboundSchema: z.ZodMiniType<RevokeUserSessionSecurity$Outbound, RevokeUserSessionSecurity>;
export declare function revokeUserSessionSecurityToJSON(revokeUserSessionSecurity: RevokeUserSessionSecurity): string;
/** @internal */
export type RevokeUserSessionRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const RevokeUserSessionRequest$outboundSchema: z.ZodMiniType<RevokeUserSessionRequest$Outbound, RevokeUserSessionRequest>;
export declare function revokeUserSessionRequestToJSON(revokeUserSessionRequest: RevokeUserSessionRequest): string;
//# sourceMappingURL=revokeusersession.d.ts.map