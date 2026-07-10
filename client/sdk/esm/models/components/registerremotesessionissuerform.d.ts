import * as z from "zod/v4-mini";
/**
 * Form for registering a new remote_session_client against an existing remote_session_issuer via RFC 7591 Dynamic Client Registration.
 */
export type RegisterRemoteSessionIssuerForm = {
    /**
     * Optional client_name to send in the RFC 7591 registration request.
     */
    clientName?: string | undefined;
    /**
     * Optional redirect_uris to send in the RFC 7591 registration request.
     */
    redirectUris?: Array<string> | undefined;
    /**
     * The remote_session_issuer to register against. Must have a registration_endpoint configured.
     */
    remoteSessionIssuerId: string;
    /**
     * The user_session_issuer the issued client is paired with.
     */
    userSessionIssuerId: string;
};
/** @internal */
export type RegisterRemoteSessionIssuerForm$Outbound = {
    client_name?: string | undefined;
    redirect_uris?: Array<string> | undefined;
    remote_session_issuer_id: string;
    user_session_issuer_id: string;
};
/** @internal */
export declare const RegisterRemoteSessionIssuerForm$outboundSchema: z.ZodMiniType<RegisterRemoteSessionIssuerForm$Outbound, RegisterRemoteSessionIssuerForm>;
export declare function registerRemoteSessionIssuerFormToJSON(registerRemoteSessionIssuerForm: RegisterRemoteSessionIssuerForm): string;
//# sourceMappingURL=registerremotesessionissuerform.d.ts.map