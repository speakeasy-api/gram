import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
 */
export declare const TokenEndpointAuthMethod: {
    readonly ClientSecretBasic: "client_secret_basic";
    readonly ClientSecretPost: "client_secret_post";
    readonly None: "none";
};
/**
 * How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
 */
export type TokenEndpointAuthMethod = ClosedEnum<typeof TokenEndpointAuthMethod>;
/**
 * Form for creating a global remote_session_client. Caller supplies client_id (and optional client_secret) obtained out-of-band from the upstream issuer.
 */
export type CreateGlobalRemoteSessionClientForm = {
    /**
     * Optional upstream OAuth audience to send on the authorize redirect and token exchange.
     */
    audience?: string | undefined;
    /**
     * client_id supplied by the caller.
     */
    clientId: string;
    /**
     * client_secret supplied by the caller. Gram encrypts before persisting.
     */
    clientSecret?: string | undefined;
    /**
     * The owning global remote_session_issuer id.
     */
    remoteSessionIssuerId: string;
    /**
     * Explicit upstream OAuth scopes the dance should request for this client. Omit to fall back to the issuer's scopes_supported.
     */
    scope?: Array<string> | undefined;
    /**
     * How the client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
     */
    tokenEndpointAuthMethod?: TokenEndpointAuthMethod | undefined;
};
/** @internal */
export declare const TokenEndpointAuthMethod$outboundSchema: z.ZodMiniEnum<typeof TokenEndpointAuthMethod>;
/** @internal */
export type CreateGlobalRemoteSessionClientForm$Outbound = {
    audience?: string | undefined;
    client_id: string;
    client_secret?: string | undefined;
    remote_session_issuer_id: string;
    scope?: Array<string> | undefined;
    token_endpoint_auth_method?: string | undefined;
};
/** @internal */
export declare const CreateGlobalRemoteSessionClientForm$outboundSchema: z.ZodMiniType<CreateGlobalRemoteSessionClientForm$Outbound, CreateGlobalRemoteSessionClientForm>;
export declare function createGlobalRemoteSessionClientFormToJSON(createGlobalRemoteSessionClientForm: CreateGlobalRemoteSessionClientForm): string;
//# sourceMappingURL=createglobalremotesessionclientform.d.ts.map