import * as z from "zod/v4-mini";
export type OAuthProxyServerUpdateForm = {
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
   * OAuth scopes to request (omit = no change, empty array = clear)
   */
  scopesSupported?: Array<string> | undefined;
  /**
   * The token endpoint URL
   */
  tokenEndpoint?: string | undefined;
  /**
   * Auth methods (omit = no change, empty array = clear)
   */
  tokenEndpointAuthMethodsSupported?: Array<string> | undefined;
};
/** @internal */
export type OAuthProxyServerUpdateForm$Outbound = {
  audience?: string | undefined;
  authorization_endpoint?: string | undefined;
  environment_slug?: string | undefined;
  scopes_supported?: Array<string> | undefined;
  token_endpoint?: string | undefined;
  token_endpoint_auth_methods_supported?: Array<string> | undefined;
};
/** @internal */
export declare const OAuthProxyServerUpdateForm$outboundSchema: z.ZodMiniType<
  OAuthProxyServerUpdateForm$Outbound,
  OAuthProxyServerUpdateForm
>;
export declare function oAuthProxyServerUpdateFormToJSON(
  oAuthProxyServerUpdateForm: OAuthProxyServerUpdateForm,
): string;
//# sourceMappingURL=oauthproxyserverupdateform.d.ts.map
