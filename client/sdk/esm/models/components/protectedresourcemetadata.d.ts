import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * RFC 9728 OAuth Protected Resource Metadata advertised by a remote MCP server. Only fields the dashboard renders are typed; the RFC allows additional members.
 */
export type ProtectedResourceMetadata = {
    /**
     * Authorization servers that can issue access tokens for this resource.
     */
    authorizationServers?: Array<string> | undefined;
    /**
     * Bearer token presentation methods accepted by the resource server.
     */
    bearerMethodsSupported?: Array<string> | undefined;
    /**
     * The resource server's identifier.
     */
    resource?: string | undefined;
    /**
     * URL of human-readable documentation for the resource server.
     */
    resourceDocumentation?: string | undefined;
    /**
     * Scopes advertised by the resource server.
     */
    scopesSupported?: Array<string> | undefined;
};
/** @internal */
export declare const ProtectedResourceMetadata$inboundSchema: z.ZodMiniType<ProtectedResourceMetadata, unknown>;
export declare function protectedResourceMetadataFromJSON(jsonString: string): SafeParseResult<ProtectedResourceMetadata, SDKValidationError>;
//# sourceMappingURL=protectedresourcemetadata.d.ts.map