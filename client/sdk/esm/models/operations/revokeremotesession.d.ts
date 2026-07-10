import * as z from "zod/v4-mini";
export type RevokeRemoteSessionSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RevokeRemoteSessionSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RevokeRemoteSessionSecurity = {
    option1?: RevokeRemoteSessionSecurityOption1 | undefined;
    option2?: RevokeRemoteSessionSecurityOption2 | undefined;
};
export type RevokeRemoteSessionRequest = {
    /**
     * The remote_session id.
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
export type RevokeRemoteSessionSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RevokeRemoteSessionSecurityOption1$outboundSchema: z.ZodMiniType<RevokeRemoteSessionSecurityOption1$Outbound, RevokeRemoteSessionSecurityOption1>;
export declare function revokeRemoteSessionSecurityOption1ToJSON(revokeRemoteSessionSecurityOption1: RevokeRemoteSessionSecurityOption1): string;
/** @internal */
export type RevokeRemoteSessionSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RevokeRemoteSessionSecurityOption2$outboundSchema: z.ZodMiniType<RevokeRemoteSessionSecurityOption2$Outbound, RevokeRemoteSessionSecurityOption2>;
export declare function revokeRemoteSessionSecurityOption2ToJSON(revokeRemoteSessionSecurityOption2: RevokeRemoteSessionSecurityOption2): string;
/** @internal */
export type RevokeRemoteSessionSecurity$Outbound = {
    Option1?: RevokeRemoteSessionSecurityOption1$Outbound | undefined;
    Option2?: RevokeRemoteSessionSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RevokeRemoteSessionSecurity$outboundSchema: z.ZodMiniType<RevokeRemoteSessionSecurity$Outbound, RevokeRemoteSessionSecurity>;
export declare function revokeRemoteSessionSecurityToJSON(revokeRemoteSessionSecurity: RevokeRemoteSessionSecurity): string;
/** @internal */
export type RevokeRemoteSessionRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const RevokeRemoteSessionRequest$outboundSchema: z.ZodMiniType<RevokeRemoteSessionRequest$Outbound, RevokeRemoteSessionRequest>;
export declare function revokeRemoteSessionRequestToJSON(revokeRemoteSessionRequest: RevokeRemoteSessionRequest): string;
//# sourceMappingURL=revokeremotesession.d.ts.map