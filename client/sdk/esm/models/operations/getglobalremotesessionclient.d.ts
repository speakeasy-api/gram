import * as z from "zod/v4-mini";
export type GetGlobalRemoteSessionClientSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type GetGlobalRemoteSessionClientRequest = {
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
export type GetGlobalRemoteSessionClientSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetGlobalRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<GetGlobalRemoteSessionClientSecurity$Outbound, GetGlobalRemoteSessionClientSecurity>;
export declare function getGlobalRemoteSessionClientSecurityToJSON(getGlobalRemoteSessionClientSecurity: GetGlobalRemoteSessionClientSecurity): string;
/** @internal */
export type GetGlobalRemoteSessionClientRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetGlobalRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<GetGlobalRemoteSessionClientRequest$Outbound, GetGlobalRemoteSessionClientRequest>;
export declare function getGlobalRemoteSessionClientRequestToJSON(getGlobalRemoteSessionClientRequest: GetGlobalRemoteSessionClientRequest): string;
//# sourceMappingURL=getglobalremotesessionclient.d.ts.map