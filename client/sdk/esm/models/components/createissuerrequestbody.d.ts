import * as z from "zod/v4-mini";
export type CreateIssuerRequestBody = {
  /**
   * Upstream authorization endpoint.
   */
  authorizationEndpoint?: string | undefined;
  /**
   * When true, the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft). Discovered from the issuer metadata document and used to pre-flight outbound CIMD. Default false.
   */
  clientIdMetadataDocumentSupported?: boolean | undefined;
  /**
   * Grant types advertised by the issuer.
   */
  grantTypesSupported?: Array<string> | undefined;
  /**
   * Issuer URL; matches the iss claim.
   */
  issuer: string;
  /**
   * Upstream JWKS URI.
   */
  jwksUri?: string | undefined;
  /**
   * Optional logo asset id.
   */
  logoAssetId?: string | undefined;
  /**
   * Optional display name. Stored NULL when empty; clients fall back to the issuer URL/slug.
   */
  name?: string | undefined;
  /**
   * When true, may unlock OIDC-aware behaviour. Default false.
   */
  oidc?: boolean | undefined;
  /**
   * When true, the MCP client registers and transacts directly with this issuer. Default false.
   */
  passthrough?: boolean | undefined;
  /**
   * Owning project id; the project must belong to the caller's organization. Omit to create an organization-level issuer.
   */
  projectId?: string | undefined;
  /**
   * Upstream RFC 7591 registration endpoint; absent for issuers without DCR.
   */
  registrationEndpoint?: string | undefined;
  /**
   * Response types advertised by the issuer.
   */
  responseTypesSupported?: Array<string> | undefined;
  /**
   * Scopes advertised by the issuer.
   */
  scopesSupported?: Array<string> | undefined;
  /**
   * Project-unique slug.
   */
  slug: string;
  /**
   * Upstream token endpoint.
   */
  tokenEndpoint?: string | undefined;
  /**
   * Token endpoint auth methods advertised by the issuer.
   */
  tokenEndpointAuthMethodsSupported?: Array<string> | undefined;
};
/** @internal */
export type CreateIssuerRequestBody$Outbound = {
  authorization_endpoint?: string | undefined;
  client_id_metadata_document_supported?: boolean | undefined;
  grant_types_supported?: Array<string> | undefined;
  issuer: string;
  jwks_uri?: string | undefined;
  logo_asset_id?: string | undefined;
  name?: string | undefined;
  oidc?: boolean | undefined;
  passthrough?: boolean | undefined;
  project_id?: string | undefined;
  registration_endpoint?: string | undefined;
  response_types_supported?: Array<string> | undefined;
  scopes_supported?: Array<string> | undefined;
  slug: string;
  token_endpoint?: string | undefined;
  token_endpoint_auth_methods_supported?: Array<string> | undefined;
};
/** @internal */
export declare const CreateIssuerRequestBody$outboundSchema: z.ZodMiniType<
  CreateIssuerRequestBody$Outbound,
  CreateIssuerRequestBody
>;
export declare function createIssuerRequestBodyToJSON(
  createIssuerRequestBody: CreateIssuerRequestBody,
): string;
//# sourceMappingURL=createissuerrequestbody.d.ts.map
