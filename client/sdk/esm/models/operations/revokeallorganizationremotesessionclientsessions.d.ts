import * as z from "zod/v4-mini";
export type RevokeAllOrganizationRemoteSessionClientSessionsSecurity = {
    sessionHeaderGramSession?: string | undefined;
    apikeyHeaderGramKey?: string | undefined;
};
export type RevokeAllOrganizationRemoteSessionClientSessionsRequest = {
    /**
     * The remote_session_client id.
     */
    clientId: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
};
/** @internal */
export type RevokeAllOrganizationRemoteSessionClientSessionsSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const RevokeAllOrganizationRemoteSessionClientSessionsSecurity$outboundSchema: z.ZodMiniType<RevokeAllOrganizationRemoteSessionClientSessionsSecurity$Outbound, RevokeAllOrganizationRemoteSessionClientSessionsSecurity>;
export declare function revokeAllOrganizationRemoteSessionClientSessionsSecurityToJSON(revokeAllOrganizationRemoteSessionClientSessionsSecurity: RevokeAllOrganizationRemoteSessionClientSessionsSecurity): string;
/** @internal */
export type RevokeAllOrganizationRemoteSessionClientSessionsRequest$Outbound = {
    client_id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const RevokeAllOrganizationRemoteSessionClientSessionsRequest$outboundSchema: z.ZodMiniType<RevokeAllOrganizationRemoteSessionClientSessionsRequest$Outbound, RevokeAllOrganizationRemoteSessionClientSessionsRequest>;
export declare function revokeAllOrganizationRemoteSessionClientSessionsRequestToJSON(revokeAllOrganizationRemoteSessionClientSessionsRequest: RevokeAllOrganizationRemoteSessionClientSessionsRequest): string;
//# sourceMappingURL=revokeallorganizationremotesessionclientsessions.d.ts.map