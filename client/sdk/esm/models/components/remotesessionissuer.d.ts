import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A remote_session_issuer record — upstream Authorization Server identity that Gram speaks OAuth to.
 */
export type RemoteSessionIssuer = {
    /**
     * Upstream authorization endpoint.
     */
    authorizationEndpoint?: string | undefined;
    /**
     * Whether the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft).
     */
    clientIdMetadataDocumentSupported: boolean;
    createdAt: Date;
    grantTypesSupported?: Array<string> | undefined;
    /**
     * The remote_session_issuer id.
     */
    id: string;
    /**
     * Issuer URL; matches the iss claim.
     */
    issuer: string;
    /**
     * Upstream JWKS URI; null when not advertised.
     */
    jwksUri?: string | undefined;
    /**
     * Optional logo asset id; null when unset.
     */
    logoAssetId?: string | undefined;
    /**
     * Optional display name; null when unset.
     */
    name?: string | undefined;
    /**
     * When true, may unlock OIDC-aware behaviour.
     */
    oidc: boolean;
    /**
     * The owning organization id. Empty for legacy rows not yet backfilled.
     */
    organizationId: string;
    /**
     * When true, the MCP client registers and transacts directly with this issuer.
     */
    passthrough: boolean;
    /**
     * The owning project id. Empty for organization-level issuers.
     */
    projectId: string;
    /**
     * Upstream RFC 7591 registration endpoint; null for issuers without DCR.
     */
    registrationEndpoint?: string | undefined;
    responseTypesSupported?: Array<string> | undefined;
    scopesSupported?: Array<string> | undefined;
    /**
     * Project-unique slug.
     */
    slug: string;
    /**
     * Upstream token endpoint.
     */
    tokenEndpoint?: string | undefined;
    tokenEndpointAuthMethodsSupported?: Array<string> | undefined;
    updatedAt: Date;
};
/** @internal */
export declare const RemoteSessionIssuer$inboundSchema: z.ZodMiniType<RemoteSessionIssuer, unknown>;
export declare function remoteSessionIssuerFromJSON(jsonString: string): SafeParseResult<RemoteSessionIssuer, SDKValidationError>;
//# sourceMappingURL=remotesessionissuer.d.ts.map