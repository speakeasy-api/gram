import * as z from "zod/v4-mini";
export type DeleteRemoteSessionClientSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DeleteRemoteSessionClientSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DeleteRemoteSessionClientSecurity = {
    option1?: DeleteRemoteSessionClientSecurityOption1 | undefined;
    option2?: DeleteRemoteSessionClientSecurityOption2 | undefined;
};
export type DeleteRemoteSessionClientRequest = {
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
export type DeleteRemoteSessionClientSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteRemoteSessionClientSecurityOption1$outboundSchema: z.ZodMiniType<DeleteRemoteSessionClientSecurityOption1$Outbound, DeleteRemoteSessionClientSecurityOption1>;
export declare function deleteRemoteSessionClientSecurityOption1ToJSON(deleteRemoteSessionClientSecurityOption1: DeleteRemoteSessionClientSecurityOption1): string;
/** @internal */
export type DeleteRemoteSessionClientSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteRemoteSessionClientSecurityOption2$outboundSchema: z.ZodMiniType<DeleteRemoteSessionClientSecurityOption2$Outbound, DeleteRemoteSessionClientSecurityOption2>;
export declare function deleteRemoteSessionClientSecurityOption2ToJSON(deleteRemoteSessionClientSecurityOption2: DeleteRemoteSessionClientSecurityOption2): string;
/** @internal */
export type DeleteRemoteSessionClientSecurity$Outbound = {
    Option1?: DeleteRemoteSessionClientSecurityOption1$Outbound | undefined;
    Option2?: DeleteRemoteSessionClientSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<DeleteRemoteSessionClientSecurity$Outbound, DeleteRemoteSessionClientSecurity>;
export declare function deleteRemoteSessionClientSecurityToJSON(deleteRemoteSessionClientSecurity: DeleteRemoteSessionClientSecurity): string;
/** @internal */
export type DeleteRemoteSessionClientRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<DeleteRemoteSessionClientRequest$Outbound, DeleteRemoteSessionClientRequest>;
export declare function deleteRemoteSessionClientRequestToJSON(deleteRemoteSessionClientRequest: DeleteRemoteSessionClientRequest): string;
//# sourceMappingURL=deleteremotesessionclient.d.ts.map