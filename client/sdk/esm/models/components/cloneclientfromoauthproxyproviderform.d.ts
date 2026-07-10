import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * How the cloned client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
 */
export declare const CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod: {
    readonly ClientSecretBasic: "client_secret_basic";
    readonly ClientSecretPost: "client_secret_post";
    readonly None: "none";
};
/**
 * How the cloned client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
 */
export type CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod = ClosedEnum<typeof CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod>;
/**
 * Form for cloning an oauth_proxy_provider's client credentials into a new remote_session_client. The caller supplies the existing oauth_proxy_provider and the remote_session_issuer to register the new client with, plus zero or more user_session_issuers to attach it to.
 */
export type CloneClientFromOAuthProxyProviderForm = {
    /**
     * Optional upstream OAuth audience to send on the authorize redirect and token exchange for the cloned client.
     */
    audience?: string | undefined;
    /**
     * The oauth_proxy_provider to read client_id / client_secret from. Must live in the caller's project.
     */
    oauthProxyProviderId: string;
    /**
     * The remote_session_issuer the new client is registered with.
     */
    remoteSessionIssuerId: string;
    /**
     * Explicit upstream OAuth scopes the dance should request for the cloned client. Omit to fall back to the issuer's scopes_supported.
     */
    scope?: Array<string> | undefined;
    /**
     * How the cloned client authenticates at the issuer's token endpoint. Omit to default to client_secret_basic.
     */
    tokenEndpointAuthMethod?: CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod | undefined;
    /**
     * The user_session_issuers to attach the new client to via the join table. Omit or pass an empty array to clone a standalone client with no attachments.
     */
    userSessionIssuerIds?: Array<string> | undefined;
};
/** @internal */
export declare const CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod$outboundSchema: z.ZodMiniEnum<typeof CloneClientFromOAuthProxyProviderFormTokenEndpointAuthMethod>;
/** @internal */
export type CloneClientFromOAuthProxyProviderForm$Outbound = {
    audience?: string | undefined;
    oauth_proxy_provider_id: string;
    remote_session_issuer_id: string;
    scope?: Array<string> | undefined;
    token_endpoint_auth_method?: string | undefined;
    user_session_issuer_ids?: Array<string> | undefined;
};
/** @internal */
export declare const CloneClientFromOAuthProxyProviderForm$outboundSchema: z.ZodMiniType<CloneClientFromOAuthProxyProviderForm$Outbound, CloneClientFromOAuthProxyProviderForm>;
export declare function cloneClientFromOAuthProxyProviderFormToJSON(cloneClientFromOAuthProxyProviderForm: CloneClientFromOAuthProxyProviderForm): string;
//# sourceMappingURL=cloneclientfromoauthproxyproviderform.d.ts.map