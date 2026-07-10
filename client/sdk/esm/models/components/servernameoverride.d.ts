import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * User-defined display name for a hooks server
 */
export type ServerNameOverride = {
  /**
   * User-friendly display name
   */
  displayName: string;
  /**
   * Override ID
   */
  id: string;
  /**
   * Original server name from hooks
   */
  rawServerName: string;
};
/** @internal */
export declare const ServerNameOverride$inboundSchema: z.ZodMiniType<
  ServerNameOverride,
  unknown
>;
export declare function serverNameOverrideFromJSON(
  jsonString: string,
): SafeParseResult<ServerNameOverride, SDKValidationError>;
//# sourceMappingURL=servernameoverride.d.ts.map
