import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A draft remote_session_issuer returned by discover. Same shape as RemoteSessionIssuer minus id/project_id/timestamps, plus discovery_warnings describing any RFC 8414 deviations.
 */
export type RemoteSessionIssuerDraft = {
  /**
   * Upstream authorization endpoint.
   */
  authorizationEndpoint?: string | undefined;
  /**
   * Whether the issuer advertises support for a Client ID Metadata Document URL as client_id (OAuth CIMD draft), parsed from the discovery document.
   */
  clientIdMetadataDocumentSupported: boolean;
  /**
   * Warnings describing any RFC 8414 deviations encountered during discovery.
   */
  discoveryWarnings: Array<string>;
  grantTypesSupported?: Array<string> | undefined;
  /**
   * Issuer URL; matches the iss claim.
   */
  issuer: string;
  /**
   * Upstream JWKS URI; null when not advertised.
   */
  jwksUri?: string | undefined;
  /**
   * When true, may unlock OIDC-aware behaviour.
   */
  oidc: boolean;
  /**
   * When true, the MCP client registers and transacts directly with this issuer.
   */
  passthrough: boolean;
  /**
   * Upstream RFC 7591 registration endpoint; null for issuers without DCR.
   */
  registrationEndpoint?: string | undefined;
  responseTypesSupported?: Array<string> | undefined;
  scopesSupported?: Array<string> | undefined;
  /**
   * Upstream token endpoint.
   */
  tokenEndpoint?: string | undefined;
  tokenEndpointAuthMethodsSupported?: Array<string> | undefined;
};
/** @internal */
export declare const RemoteSessionIssuerDraft$inboundSchema: z.ZodMiniType<
  RemoteSessionIssuerDraft,
  unknown
>;
export declare function remoteSessionIssuerDraftFromJSON(
  jsonString: string,
): SafeParseResult<RemoteSessionIssuerDraft, SDKValidationError>;
//# sourceMappingURL=remotesessionissuerdraft.d.ts.map
