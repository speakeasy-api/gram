import * as z from "zod/v4-mini";
export type GetOrganizationRemoteSessionClientDeletePreflightSecurity = {
    sessionHeaderGramSession?: string | undefined;
    apikeyHeaderGramKey?: string | undefined;
};
export type GetOrganizationRemoteSessionClientDeletePreflightRequest = {
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
};
/** @internal */
export type GetOrganizationRemoteSessionClientDeletePreflightSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRemoteSessionClientDeletePreflightSecurity$outboundSchema: z.ZodMiniType<GetOrganizationRemoteSessionClientDeletePreflightSecurity$Outbound, GetOrganizationRemoteSessionClientDeletePreflightSecurity>;
export declare function getOrganizationRemoteSessionClientDeletePreflightSecurityToJSON(getOrganizationRemoteSessionClientDeletePreflightSecurity: GetOrganizationRemoteSessionClientDeletePreflightSecurity): string;
/** @internal */
export type GetOrganizationRemoteSessionClientDeletePreflightRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRemoteSessionClientDeletePreflightRequest$outboundSchema: z.ZodMiniType<GetOrganizationRemoteSessionClientDeletePreflightRequest$Outbound, GetOrganizationRemoteSessionClientDeletePreflightRequest>;
export declare function getOrganizationRemoteSessionClientDeletePreflightRequestToJSON(getOrganizationRemoteSessionClientDeletePreflightRequest: GetOrganizationRemoteSessionClientDeletePreflightRequest): string;
//# sourceMappingURL=getorganizationremotesessionclientdeletepreflight.d.ts.map