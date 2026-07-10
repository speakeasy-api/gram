import * as z from "zod/v4-mini";
export type GetOrganizationRemoteSessionIssuerDeletePreflightSecurity = {
    sessionHeaderGramSession?: string | undefined;
    apikeyHeaderGramKey?: string | undefined;
};
export type GetOrganizationRemoteSessionIssuerDeletePreflightRequest = {
    /**
     * The remote_session_issuer id.
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
export type GetOrganizationRemoteSessionIssuerDeletePreflightSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRemoteSessionIssuerDeletePreflightSecurity$outboundSchema: z.ZodMiniType<GetOrganizationRemoteSessionIssuerDeletePreflightSecurity$Outbound, GetOrganizationRemoteSessionIssuerDeletePreflightSecurity>;
export declare function getOrganizationRemoteSessionIssuerDeletePreflightSecurityToJSON(getOrganizationRemoteSessionIssuerDeletePreflightSecurity: GetOrganizationRemoteSessionIssuerDeletePreflightSecurity): string;
/** @internal */
export type GetOrganizationRemoteSessionIssuerDeletePreflightRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRemoteSessionIssuerDeletePreflightRequest$outboundSchema: z.ZodMiniType<GetOrganizationRemoteSessionIssuerDeletePreflightRequest$Outbound, GetOrganizationRemoteSessionIssuerDeletePreflightRequest>;
export declare function getOrganizationRemoteSessionIssuerDeletePreflightRequestToJSON(getOrganizationRemoteSessionIssuerDeletePreflightRequest: GetOrganizationRemoteSessionIssuerDeletePreflightRequest): string;
//# sourceMappingURL=getorganizationremotesessionissuerdeletepreflight.d.ts.map