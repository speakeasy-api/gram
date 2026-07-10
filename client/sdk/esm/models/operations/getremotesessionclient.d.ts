import * as z from "zod/v4-mini";
export type GetRemoteSessionClientSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type GetRemoteSessionClientSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type GetRemoteSessionClientSecurity = {
    option1?: GetRemoteSessionClientSecurityOption1 | undefined;
    option2?: GetRemoteSessionClientSecurityOption2 | undefined;
};
export type GetRemoteSessionClientRequest = {
    /**
     * The remote_session_client id.
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
export type GetRemoteSessionClientSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRemoteSessionClientSecurityOption1$outboundSchema: z.ZodMiniType<GetRemoteSessionClientSecurityOption1$Outbound, GetRemoteSessionClientSecurityOption1>;
export declare function getRemoteSessionClientSecurityOption1ToJSON(getRemoteSessionClientSecurityOption1: GetRemoteSessionClientSecurityOption1): string;
/** @internal */
export type GetRemoteSessionClientSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRemoteSessionClientSecurityOption2$outboundSchema: z.ZodMiniType<GetRemoteSessionClientSecurityOption2$Outbound, GetRemoteSessionClientSecurityOption2>;
export declare function getRemoteSessionClientSecurityOption2ToJSON(getRemoteSessionClientSecurityOption2: GetRemoteSessionClientSecurityOption2): string;
/** @internal */
export type GetRemoteSessionClientSecurity$Outbound = {
    Option1?: GetRemoteSessionClientSecurityOption1$Outbound | undefined;
    Option2?: GetRemoteSessionClientSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<GetRemoteSessionClientSecurity$Outbound, GetRemoteSessionClientSecurity>;
export declare function getRemoteSessionClientSecurityToJSON(getRemoteSessionClientSecurity: GetRemoteSessionClientSecurity): string;
/** @internal */
export type GetRemoteSessionClientRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<GetRemoteSessionClientRequest$Outbound, GetRemoteSessionClientRequest>;
export declare function getRemoteSessionClientRequestToJSON(getRemoteSessionClientRequest: GetRemoteSessionClientRequest): string;
//# sourceMappingURL=getremotesessionclient.d.ts.map