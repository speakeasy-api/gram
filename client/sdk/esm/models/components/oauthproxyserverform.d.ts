import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The type of OAuth provider
 */
export declare const OAuthProxyServerFormProviderType: {
    readonly Custom: "custom";
    readonly Gram: "gram";
};
/**
 * The type of OAuth provider
 */
export type OAuthProxyServerFormProviderType = ClosedEnum<typeof OAuthProxyServerFormProviderType>;
export type OAuthProxyServerForm = {
    /**
     * The audience parameter to send to the upstream OAuth provider
     */
    audience?: string | undefined;
    /**
     * The authorization endpoint URL
     */
    authorizationEndpoint?: string | undefined;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    environmentSlug?: string | undefined;
    /**
     * The type of OAuth provider
     */
    providerType: OAuthProxyServerFormProviderType;
    /**
     * OAuth scopes to request
     */
    scopesSupported?: Array<string> | undefined;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    slug: string;
    /**
     * The token endpoint URL
     */
    tokenEndpoint?: string | undefined;
    /**
     * Auth methods (client_secret_basic or client_secret_post)
     */
    tokenEndpointAuthMethodsSupported?: Array<string> | undefined;
};
/** @internal */
export declare const OAuthProxyServerFormProviderType$outboundSchema: z.ZodMiniEnum<typeof OAuthProxyServerFormProviderType>;
/** @internal */
export type OAuthProxyServerForm$Outbound = {
    audience?: string | undefined;
    authorization_endpoint?: string | undefined;
    environment_slug?: string | undefined;
    provider_type: string;
    scopes_supported?: Array<string> | undefined;
    slug: string;
    token_endpoint?: string | undefined;
    token_endpoint_auth_methods_supported?: Array<string> | undefined;
};
/** @internal */
export declare const OAuthProxyServerForm$outboundSchema: z.ZodMiniType<OAuthProxyServerForm$Outbound, OAuthProxyServerForm>;
export declare function oAuthProxyServerFormToJSON(oAuthProxyServerForm: OAuthProxyServerForm): string;
//# sourceMappingURL=oauthproxyserverform.d.ts.map