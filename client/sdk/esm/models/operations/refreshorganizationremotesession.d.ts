import * as z from "zod/v4-mini";
export type RefreshOrganizationRemoteSessionSecurity = {
    sessionHeaderGramSession?: string | undefined;
    apikeyHeaderGramKey?: string | undefined;
};
export type RefreshOrganizationRemoteSessionRequest = {
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
};
/** @internal */
export type RefreshOrganizationRemoteSessionSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const RefreshOrganizationRemoteSessionSecurity$outboundSchema: z.ZodMiniType<RefreshOrganizationRemoteSessionSecurity$Outbound, RefreshOrganizationRemoteSessionSecurity>;
export declare function refreshOrganizationRemoteSessionSecurityToJSON(refreshOrganizationRemoteSessionSecurity: RefreshOrganizationRemoteSessionSecurity): string;
/** @internal */
export type RefreshOrganizationRemoteSessionRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const RefreshOrganizationRemoteSessionRequest$outboundSchema: z.ZodMiniType<RefreshOrganizationRemoteSessionRequest$Outbound, RefreshOrganizationRemoteSessionRequest>;
export declare function refreshOrganizationRemoteSessionRequestToJSON(refreshOrganizationRemoteSessionRequest: RefreshOrganizationRemoteSessionRequest): string;
//# sourceMappingURL=refreshorganizationremotesession.d.ts.map