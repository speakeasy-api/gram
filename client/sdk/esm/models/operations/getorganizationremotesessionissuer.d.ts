import * as z from "zod/v4-mini";
export type GetOrganizationRemoteSessionIssuerSecurity = {
    sessionHeaderGramSession?: string | undefined;
    apikeyHeaderGramKey?: string | undefined;
};
export type GetOrganizationRemoteSessionIssuerRequest = {
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
export type GetOrganizationRemoteSessionIssuerSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<GetOrganizationRemoteSessionIssuerSecurity$Outbound, GetOrganizationRemoteSessionIssuerSecurity>;
export declare function getOrganizationRemoteSessionIssuerSecurityToJSON(getOrganizationRemoteSessionIssuerSecurity: GetOrganizationRemoteSessionIssuerSecurity): string;
/** @internal */
export type GetOrganizationRemoteSessionIssuerRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const GetOrganizationRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<GetOrganizationRemoteSessionIssuerRequest$Outbound, GetOrganizationRemoteSessionIssuerRequest>;
export declare function getOrganizationRemoteSessionIssuerRequestToJSON(getOrganizationRemoteSessionIssuerRequest: GetOrganizationRemoteSessionIssuerRequest): string;
//# sourceMappingURL=getorganizationremotesessionissuer.d.ts.map