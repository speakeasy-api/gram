import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ExternalOAuthServer = {
    /**
     * When the external OAuth server was created.
     */
    createdAt: Date;
    /**
     * The ID of the external OAuth server
     */
    id: string;
    /**
     * The metadata for the external OAuth server
     */
    metadata: any;
    /**
     * The project ID this external OAuth server belongs to
     */
    projectId: string;
    /**
     * A short url-friendly label that uniquely identifies a resource.
     */
    slug: string;
    /**
     * When the external OAuth server was last updated.
     */
    updatedAt: Date;
};
/** @internal */
export declare const ExternalOAuthServer$inboundSchema: z.ZodMiniType<ExternalOAuthServer, unknown>;
export declare function externalOAuthServerFromJSON(jsonString: string): SafeParseResult<ExternalOAuthServer, SDKValidationError>;
//# sourceMappingURL=externaloauthserver.d.ts.map