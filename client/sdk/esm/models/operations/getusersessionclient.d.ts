import * as z from "zod/v4-mini";
export type GetUserSessionClientSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetUserSessionClientSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetUserSessionClientSecurity = {
    option1?: GetUserSessionClientSecurityOption1 | undefined;
    option2?: GetUserSessionClientSecurityOption2 | undefined;
};
export type GetUserSessionClientRequest = {
    /**
     * The user_session_client id.
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
export type GetUserSessionClientSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetUserSessionClientSecurityOption1$outboundSchema: z.ZodMiniType<GetUserSessionClientSecurityOption1$Outbound, GetUserSessionClientSecurityOption1>;
export declare function getUserSessionClientSecurityOption1ToJSON(getUserSessionClientSecurityOption1: GetUserSessionClientSecurityOption1): string;
/** @internal */
export type GetUserSessionClientSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetUserSessionClientSecurityOption2$outboundSchema: z.ZodMiniType<GetUserSessionClientSecurityOption2$Outbound, GetUserSessionClientSecurityOption2>;
export declare function getUserSessionClientSecurityOption2ToJSON(getUserSessionClientSecurityOption2: GetUserSessionClientSecurityOption2): string;
/** @internal */
export type GetUserSessionClientSecurity$Outbound = {
    Option1?: GetUserSessionClientSecurityOption1$Outbound | undefined;
    Option2?: GetUserSessionClientSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetUserSessionClientSecurity$outboundSchema: z.ZodMiniType<GetUserSessionClientSecurity$Outbound, GetUserSessionClientSecurity>;
export declare function getUserSessionClientSecurityToJSON(getUserSessionClientSecurity: GetUserSessionClientSecurity): string;
/** @internal */
export type GetUserSessionClientRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetUserSessionClientRequest$outboundSchema: z.ZodMiniType<GetUserSessionClientRequest$Outbound, GetUserSessionClientRequest>;
export declare function getUserSessionClientRequestToJSON(getUserSessionClientRequest: GetUserSessionClientRequest): string;
//# sourceMappingURL=getusersessionclient.d.ts.map