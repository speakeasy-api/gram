import * as z from "zod/v4-mini";
/**
 * Form for updating a remote_session_issuer. All non-id fields are optional patches.
 */
export type UpdateRemoteSessionIssuerForm = {
    /**
     * Upstream authorization endpoint.
     */
    authorizationEndpoint?: string | undefined;
    /**
     * Whether the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft).
     */
    clientIdMetadataDocumentSupported?: boolean | undefined;
    grantTypesSupported?: Array<string> | undefined;
    /**
     * The remote_session_issuer id.
     */
    id: string;
    /**
     * Issuer URL; matches the iss claim.
     */
    issuer?: string | undefined;
    /**
     * Upstream JWKS URI.
     */
    jwksUri?: string | undefined;
    /**
     * Set the logo asset id.
     */
    logoAssetId?: string | undefined;
    /**
     * Set or clear the display name. An empty string clears it to NULL.
     */
    name?: string | undefined;
    oidc?: boolean | undefined;
    passthrough?: boolean | undefined;
    /**
     * Upstream RFC 7591 registration endpoint.
     */
    registrationEndpoint?: string | undefined;
    responseTypesSupported?: Array<string> | undefined;
    scopesSupported?: Array<string> | undefined;
    /**
     * Rename the slug.
     */
    slug?: string | undefined;
    /**
     * Upstream token endpoint.
     */
    tokenEndpoint?: string | undefined;
    tokenEndpointAuthMethodsSupported?: Array<string> | undefined;
};
/** @internal */
export type UpdateRemoteSessionIssuerForm$Outbound = {
    authorization_endpoint?: string | undefined;
    client_id_metadata_document_supported?: boolean | undefined;
    grant_types_supported?: Array<string> | undefined;
    id: string;
    issuer?: string | undefined;
    jwks_uri?: string | undefined;
    logo_asset_id?: string | undefined;
    name?: string | undefined;
    oidc?: boolean | undefined;
    passthrough?: boolean | undefined;
    registration_endpoint?: string | undefined;
    response_types_supported?: Array<string> | undefined;
    scopes_supported?: Array<string> | undefined;
    slug?: string | undefined;
    token_endpoint?: string | undefined;
    token_endpoint_auth_methods_supported?: Array<string> | undefined;
};
/** @internal */
export declare const UpdateRemoteSessionIssuerForm$outboundSchema: z.ZodMiniType<UpdateRemoteSessionIssuerForm$Outbound, UpdateRemoteSessionIssuerForm>;
export declare function updateRemoteSessionIssuerFormToJSON(updateRemoteSessionIssuerForm: UpdateRemoteSessionIssuerForm): string;
//# sourceMappingURL=updateremotesessionissuerform.d.ts.map