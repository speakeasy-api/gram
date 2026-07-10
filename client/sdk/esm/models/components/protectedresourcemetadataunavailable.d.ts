import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Reason an RFC 9728 protected resource metadata probe was unavailable. Surfaced when available is false.
 */
export type ProtectedResourceMetadataUnavailable = {
  /**
   * Machine-readable failure code (e.g. not_found, http_error, transport_error, timeout, malformed, host_blocked, invalid_url). Intentionally a free-form string so adding new failure modes is not a breaking SDK change.
   */
  code: string;
  /**
   * Human-readable summary of the unavailability reason, composed by the backend. Dashboards should render verbatim.
   */
  message: string;
};
/** @internal */
export declare const ProtectedResourceMetadataUnavailable$inboundSchema: z.ZodMiniType<
  ProtectedResourceMetadataUnavailable,
  unknown
>;
export declare function protectedResourceMetadataUnavailableFromJSON(
  jsonString: string,
): SafeParseResult<ProtectedResourceMetadataUnavailable, SDKValidationError>;
//# sourceMappingURL=protectedresourcemetadataunavailable.d.ts.map
