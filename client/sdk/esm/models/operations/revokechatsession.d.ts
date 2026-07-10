import * as z from "zod/v4-mini";
export type RevokeChatSessionSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RevokeChatSessionSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RevokeChatSessionSecurity = {
    option1?: RevokeChatSessionSecurityOption1 | undefined;
    option2?: RevokeChatSessionSecurityOption2 | undefined;
};
export type RevokeChatSessionRequest = {
    /**
     * The chat session token to revoke
     */
    token: string;
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
export type RevokeChatSessionSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RevokeChatSessionSecurityOption1$outboundSchema: z.ZodMiniType<RevokeChatSessionSecurityOption1$Outbound, RevokeChatSessionSecurityOption1>;
export declare function revokeChatSessionSecurityOption1ToJSON(revokeChatSessionSecurityOption1: RevokeChatSessionSecurityOption1): string;
/** @internal */
export type RevokeChatSessionSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RevokeChatSessionSecurityOption2$outboundSchema: z.ZodMiniType<RevokeChatSessionSecurityOption2$Outbound, RevokeChatSessionSecurityOption2>;
export declare function revokeChatSessionSecurityOption2ToJSON(revokeChatSessionSecurityOption2: RevokeChatSessionSecurityOption2): string;
/** @internal */
export type RevokeChatSessionSecurity$Outbound = {
    Option1?: RevokeChatSessionSecurityOption1$Outbound | undefined;
    Option2?: RevokeChatSessionSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RevokeChatSessionSecurity$outboundSchema: z.ZodMiniType<RevokeChatSessionSecurity$Outbound, RevokeChatSessionSecurity>;
export declare function revokeChatSessionSecurityToJSON(revokeChatSessionSecurity: RevokeChatSessionSecurity): string;
/** @internal */
export type RevokeChatSessionRequest$Outbound = {
    token: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const RevokeChatSessionRequest$outboundSchema: z.ZodMiniType<RevokeChatSessionRequest$Outbound, RevokeChatSessionRequest>;
export declare function revokeChatSessionRequestToJSON(revokeChatSessionRequest: RevokeChatSessionRequest): string;
//# sourceMappingURL=revokechatsession.d.ts.map