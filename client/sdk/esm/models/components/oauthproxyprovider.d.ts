import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The type of OAuth provider
 */
export declare const ProviderType: {
  readonly Custom: "custom";
  readonly Gram: "gram";
};
/**
 * The type of OAuth provider
 */
export type ProviderType = ClosedEnum<typeof ProviderType>;
export type OAuthProxyProvider = {
  /**
   * The authorization endpoint URL
   */
  authorizationEndpoint: string;
  /**
   * When the OAuth proxy provider was created.
   */
  createdAt: Date;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  environmentSlug?: string | undefined;
  /**
   * The grant types supported by this provider
   */
  grantTypesSupported?: Array<string> | undefined;
  /**
   * The ID of the OAuth proxy provider
   */
  id: string;
  /**
   * The type of OAuth provider
   */
  providerType: ProviderType;
  /**
   * The OAuth scopes supported by this provider
   */
  scopesSupported?: Array<string> | undefined;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
  /**
   * The token endpoint URL
   */
  tokenEndpoint: string;
  /**
   * The token endpoint auth methods supported by this provider
   */
  tokenEndpointAuthMethodsSupported?: Array<string> | undefined;
  /**
   * When the OAuth proxy provider was last updated.
   */
  updatedAt: Date;
};
/** @internal */
export declare const ProviderType$inboundSchema: z.ZodMiniEnum<
  typeof ProviderType
>;
/** @internal */
export declare const OAuthProxyProvider$inboundSchema: z.ZodMiniType<
  OAuthProxyProvider,
  unknown
>;
export declare function oAuthProxyProviderFromJSON(
  jsonString: string,
): SafeParseResult<OAuthProxyProvider, SDKValidationError>;
//# sourceMappingURL=oauthproxyprovider.d.ts.map
