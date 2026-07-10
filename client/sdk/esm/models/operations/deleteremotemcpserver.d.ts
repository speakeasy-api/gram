import * as z from "zod/v4-mini";
export type DeleteRemoteMcpServerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DeleteRemoteMcpServerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DeleteRemoteMcpServerSecurity = {
    option1?: DeleteRemoteMcpServerSecurityOption1 | undefined;
    option2?: DeleteRemoteMcpServerSecurityOption2 | undefined;
};
export type DeleteRemoteMcpServerRequest = {
    /**
     * The ID of the remote MCP server to delete
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
export type DeleteRemoteMcpServerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DeleteRemoteMcpServerSecurityOption1$outboundSchema: z.ZodMiniType<DeleteRemoteMcpServerSecurityOption1$Outbound, DeleteRemoteMcpServerSecurityOption1>;
export declare function deleteRemoteMcpServerSecurityOption1ToJSON(deleteRemoteMcpServerSecurityOption1: DeleteRemoteMcpServerSecurityOption1): string;
/** @internal */
export type DeleteRemoteMcpServerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DeleteRemoteMcpServerSecurityOption2$outboundSchema: z.ZodMiniType<DeleteRemoteMcpServerSecurityOption2$Outbound, DeleteRemoteMcpServerSecurityOption2>;
export declare function deleteRemoteMcpServerSecurityOption2ToJSON(deleteRemoteMcpServerSecurityOption2: DeleteRemoteMcpServerSecurityOption2): string;
/** @internal */
export type DeleteRemoteMcpServerSecurity$Outbound = {
    Option1?: DeleteRemoteMcpServerSecurityOption1$Outbound | undefined;
    Option2?: DeleteRemoteMcpServerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DeleteRemoteMcpServerSecurity$outboundSchema: z.ZodMiniType<DeleteRemoteMcpServerSecurity$Outbound, DeleteRemoteMcpServerSecurity>;
export declare function deleteRemoteMcpServerSecurityToJSON(deleteRemoteMcpServerSecurity: DeleteRemoteMcpServerSecurity): string;
/** @internal */
export type DeleteRemoteMcpServerRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteRemoteMcpServerRequest$outboundSchema: z.ZodMiniType<DeleteRemoteMcpServerRequest$Outbound, DeleteRemoteMcpServerRequest>;
export declare function deleteRemoteMcpServerRequestToJSON(deleteRemoteMcpServerRequest: DeleteRemoteMcpServerRequest): string;
//# sourceMappingURL=deleteremotemcpserver.d.ts.map