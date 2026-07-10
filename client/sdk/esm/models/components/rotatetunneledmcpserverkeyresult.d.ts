import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { TunneledMcpServer } from "./tunneledmcpserver.js";
/**
 * Rotated tunneled MCP server plus the one-time replacement tunnel key
 */
export type RotateTunneledMcpServerKeyResult = {
  /**
   * A customer-hosted MCP server connected through a tunnel
   */
  server: TunneledMcpServer;
  /**
   * Plaintext tunnel key. Only returned after rotation.
   */
  tunnelKey: string;
};
/** @internal */
export declare const RotateTunneledMcpServerKeyResult$inboundSchema: z.ZodMiniType<
  RotateTunneledMcpServerKeyResult,
  unknown
>;
export declare function rotateTunneledMcpServerKeyResultFromJSON(
  jsonString: string,
): SafeParseResult<RotateTunneledMcpServerKeyResult, SDKValidationError>;
//# sourceMappingURL=rotatetunneledmcpserverkeyresult.d.ts.map
