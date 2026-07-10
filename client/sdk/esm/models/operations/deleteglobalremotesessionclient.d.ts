import * as z from "zod/v4-mini";
export type DeleteGlobalRemoteSessionClientSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type DeleteGlobalRemoteSessionClientRequest = {
    /**
     * The remote_session_client id.
     */
    id: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type DeleteGlobalRemoteSessionClientSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteGlobalRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<DeleteGlobalRemoteSessionClientSecurity$Outbound, DeleteGlobalRemoteSessionClientSecurity>;
export declare function deleteGlobalRemoteSessionClientSecurityToJSON(deleteGlobalRemoteSessionClientSecurity: DeleteGlobalRemoteSessionClientSecurity): string;
/** @internal */
export type DeleteGlobalRemoteSessionClientRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteGlobalRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<DeleteGlobalRemoteSessionClientRequest$Outbound, DeleteGlobalRemoteSessionClientRequest>;
export declare function deleteGlobalRemoteSessionClientRequestToJSON(deleteGlobalRemoteSessionClientRequest: DeleteGlobalRemoteSessionClientRequest): string;
//# sourceMappingURL=deleteglobalremotesessionclient.d.ts.map