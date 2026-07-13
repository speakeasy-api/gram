import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OAuthProxyProvider } from "./oauthproxyprovider.js";
export type OAuthProxyServer = {
  /**
   * The audience parameter to send to the upstream OAuth provider
   */
  audience?: string | undefined;
  /**
   * When the OAuth proxy server was created.
   */
  createdAt: Date;
  /**
   * The ID of the OAuth proxy server
   */
  id: string;
  /**
   * The OAuth proxy providers for this server
   */
  oauthProxyProviders?: Array<OAuthProxyProvider> | undefined;
  /**
   * The project ID this OAuth proxy server belongs to
   */
  projectId: string;
  /**
   * A short url-friendly label that uniquely identifies a resource.
   */
  slug: string;
  /**
   * When the OAuth proxy server was last updated.
   */
  updatedAt: Date;
};
/** @internal */
export declare const OAuthProxyServer$inboundSchema: z.ZodMiniType<
  OAuthProxyServer,
  unknown
>;
export declare function oAuthProxyServerFromJSON(
  jsonString: string,
): SafeParseResult<OAuthProxyServer, SDKValidationError>;
//# sourceMappingURL=oauthproxyserver.d.ts.map
