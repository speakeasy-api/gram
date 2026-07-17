import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ProtectedResourceMetadata } from "./protectedresourcemetadata.js";
import { ProtectedResourceMetadataUnavailable } from "./protectedresourcemetadataunavailable.js";
/**
 * Outcome of an RFC 9728 protected resource metadata probe against a remote MCP server. available=true exposes the parsed metadata; available=false exposes a typed unavailability reason. Always returned with HTTP 200 — probe failures (including 404 from upstream) are not errors at this layer because non-OAuth resource servers are an expected, normal outcome.
 */
export type ProtectedResourceMetadataDiscovery = {
  /**
   * True when the upstream advertised an RFC 9728 document. False for any unavailability reason — see the unavailable field for the cause.
   */
  available: boolean;
  /**
   * Informational deviations from RFC 9728 detected on a successful probe (e.g. missing resource field, mismatched resource value). Empty when available is false.
   */
  discoveryWarnings: Array<string>;
  /**
   * RFC 9728 OAuth Protected Resource Metadata advertised by a remote MCP server. Only fields the dashboard renders are typed; the RFC allows additional members.
   */
  metadata?: ProtectedResourceMetadata | undefined;
  /**
   * Reason an RFC 9728 protected resource metadata probe was unavailable. Surfaced when available is false.
   */
  unavailable?: ProtectedResourceMetadataUnavailable | undefined;
};
/** @internal */
export declare const ProtectedResourceMetadataDiscovery$inboundSchema: z.ZodMiniType<
  ProtectedResourceMetadataDiscovery,
  unknown
>;
export declare function protectedResourceMetadataDiscoveryFromJSON(
  jsonString: string,
): SafeParseResult<ProtectedResourceMetadataDiscovery, SDKValidationError>;
//# sourceMappingURL=protectedresourcemetadatadiscovery.d.ts.map
