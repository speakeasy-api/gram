import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A header requirement for a remote MCP server
 */
export type ExternalMCPRemoteHeader = {
    /**
     * Description of the header
     */
    description?: string | undefined;
    /**
     * Whether this header is required
     */
    isRequired?: boolean | undefined;
    /**
     * Whether this header value should be treated as secret
     */
    isSecret?: boolean | undefined;
    /**
     * Header name
     */
    name: string;
    /**
     * Placeholder value to show when collecting this header
     */
    placeholder?: string | undefined;
};
/** @internal */
export declare const ExternalMCPRemoteHeader$inboundSchema: z.ZodMiniType<ExternalMCPRemoteHeader, unknown>;
export declare function externalMCPRemoteHeaderFromJSON(jsonString: string): SafeParseResult<ExternalMCPRemoteHeader, SDKValidationError>;
//# sourceMappingURL=externalmcpremoteheader.d.ts.map